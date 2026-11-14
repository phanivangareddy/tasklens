package main

import (
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Thresholds decide when an alert fires. Percentages are of the whole machine.
type Thresholds struct {
	MemPct      float64 `json:"memPct"`      // memory in use, held for a minute
	AppPct      float64 `json:"appPct"`      // one app's share of memory, held for a minute
	SwapPct     float64 `json:"swapPct"`     // swap in use, as a share of RAM
	CPUPct      float64 `json:"cpuPct"`      // processor busy...
	CPUMin      float64 `json:"cpuMin"`      // ...for this many minutes
	DiskFreePct float64 `json:"diskFreePct"` // free space left on the system disk
	ForecastMin float64 `json:"forecastMin"` // warn when memory will run out within this many minutes
	DiskDays    float64 `json:"diskDays"`    // warn when the disk will be full within this many days
	CooldownMin float64 `json:"cooldownMin"` // don't repeat the same alert sooner than this
}

var presets = map[string]Thresholds{
	"relaxed": {MemPct: 95, AppPct: 75, SwapPct: 50, CPUPct: 95, CPUMin: 10, DiskFreePct: 5, ForecastMin: 3, DiskDays: 7, CooldownMin: 60},
	"normal":  {MemPct: 90, AppPct: 50, SwapPct: 25, CPUPct: 90, CPUMin: 5, DiskFreePct: 10, ForecastMin: 5, DiskDays: 14, CooldownMin: 30},
	"strict":  {MemPct: 85, AppPct: 35, SwapPct: 10, CPUPct: 80, CPUMin: 3, DiskFreePct: 15, ForecastMin: 10, DiskDays: 30, CooldownMin: 15},
}

type Recipient struct {
	Email        string `json:"email"`
	CriticalOnly bool   `json:"criticalOnly"`
}

type EmailSettings struct {
	Enabled    bool        `json:"enabled"`
	Provider   string      `json:"provider"` // gmail | icloud | yahoo | custom
	Host       string      `json:"host"`
	Port       int         `json:"port"`
	Username   string      `json:"username"` // also the From address
	Recipients []Recipient `json:"recipients"`
	Weekly     bool        `json:"weekly"` // also email the weekly report
	// Only used when the system keychain isn't available (some Linux setups).
	FallbackPassword string `json:"fallbackPassword,omitempty"`
}

type Settings struct {
	Popups       bool            `json:"popups"`
	Sensitivity  string          `json:"sensitivity"` // relaxed | normal | strict | custom
	Limits       Thresholds      `json:"limits"`
	Rules        map[string]bool `json:"rules"`
	Background   bool            `json:"background"` // keep watching after the dashboard is closed
	StartAtLogin bool            `json:"startAtLogin"`
	History      bool            `json:"history"` // keep two weeks of history for the Report tab
	Email        EmailSettings   `json:"email"`
}

// ruleNames lists every alert rule, in the order the dashboard shows them.
var ruleNames = []string{"memForecast", "memHigh", "appHigh", "swapHigh", "cpuHigh", "diskLow", "diskForecast"}

func defaultSettings() Settings {
	rules := map[string]bool{}
	for _, r := range ruleNames {
		rules[r] = true
	}
	return Settings{Popups: true, Sensitivity: "normal", Limits: presets["normal"], Rules: rules,
		Background: true, History: true, Email: EmailSettings{Provider: "gmail", Host: "smtp.gmail.com", Port: 587}}
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "TaskLens")
}

type settingsStore struct {
	mu sync.Mutex
	s  Settings
}

func loadSettings() *settingsStore {
	st := &settingsStore{s: defaultSettings()}
	if raw, err := os.ReadFile(filepath.Join(configDir(), "settings.json")); err == nil {
		json.Unmarshal(raw, &st.s)
	}
	for _, r := range ruleNames { // rules added in newer versions start switched on
		if _, ok := st.s.Rules[r]; !ok {
			if st.s.Rules == nil {
				st.s.Rules = map[string]bool{}
			}
			st.s.Rules[r] = true
		}
	}
	return st
}

func (st *settingsStore) get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.s
	s.Email.Recipients = append([]Recipient(nil), st.s.Email.Recipients...)
	rules := map[string]bool{}
	for k, v := range st.s.Rules {
		rules[k] = v
	}
	s.Rules = rules
	return s
}

func (st *settingsStore) save(s Settings) error {
	if p, ok := presets[s.Sensitivity]; ok {
		s.Limits = p
	} else {
		s.Sensitivity = "custom"
	}
	if err := validLimits(s.Limits); err != nil {
		return err
	}
	clean := s.Email.Recipients[:0]
	seen := map[string]bool{}
	for _, r := range s.Email.Recipients {
		r.Email = strings.TrimSpace(r.Email)
		if r.Email == "" {
			continue
		}
		if _, err := mail.ParseAddress(r.Email); err != nil || !strings.Contains(r.Email, "@") {
			return errors.New(r.Email + " doesn't look like an email address")
		}
		if !seen[strings.ToLower(r.Email)] {
			seen[strings.ToLower(r.Email)] = true
			clean = append(clean, r)
		}
	}
	s.Email.Recipients = clean
	s.Email.Username = strings.TrimSpace(s.Email.Username)
	if s.Email.Enabled && (s.Email.Host == "" || s.Email.Port == 0 || s.Email.Username == "" || len(clean) == 0) {
		return errors.New("to turn on email alerts, fill in the sender account and at least one person to alert")
	}
	st.mu.Lock()
	prev := st.s.Email
	s.Email.FallbackPassword = prev.FallbackPassword
	// A new mail server or sender must get the password typed in again, so a
	// changed setting can't quietly send the saved password somewhere else.
	moved := prev.Username != "" && (!strings.EqualFold(prev.Host, s.Email.Host) || prev.Port != s.Email.Port ||
		!strings.EqualFold(prev.Username, s.Email.Username))
	if moved {
		s.Email.FallbackPassword = ""
	}
	st.s = s
	st.mu.Unlock()
	if moved {
		forgetSecret(keyringService, prev.Username)
	}
	return st.write()
}

func validLimits(t Thresholds) error {
	in := func(v, lo, hi float64) bool { return v >= lo && v <= hi }
	if !in(t.MemPct, 50, 100) || !in(t.AppPct, 5, 100) || !in(t.SwapPct, 1, 400) || !in(t.CPUPct, 20, 100) ||
		!in(t.CPUMin, 1, 120) || !in(t.DiskFreePct, 1, 50) || !in(t.ForecastMin, 1, 60) || !in(t.DiskDays, 1, 365) ||
		!in(t.CooldownMin, 1, 1440) {
		return errors.New("one of the alert limits is out of range")
	}
	return nil
}

func (st *settingsStore) write() error {
	st.mu.Lock()
	raw, _ := json.MarshalIndent(st.s, "", "  ")
	st.mu.Unlock()
	os.MkdirAll(configDir(), 0o700)
	return os.WriteFile(filepath.Join(configDir(), "settings.json"), raw, 0o600)
}

// The email password lives in the system keychain (macOS Keychain, Windows
// Credential Manager, Linux Secret Service), never in settings.json, unless
// the keychain is missing.
const keyringService = "TaskLens email"

var forgetSecret = secretDelete // replaced in tests so they never touch the real keychain

func (st *settingsStore) setPassword(user, pw string) (inKeychain bool, err error) {
	if user == "" {
		return false, errors.New("enter the sender email address first")
	}
	if err := secretSet(keyringService, user, pw); err == nil {
		st.mu.Lock()
		st.s.Email.FallbackPassword = ""
		st.mu.Unlock()
		return true, st.write()
	}
	st.mu.Lock()
	st.s.Email.FallbackPassword = pw
	st.mu.Unlock()
	return false, st.write()
}

func (st *settingsStore) password(user string) string {
	if pw, err := secretGet(keyringService, user); err == nil {
		return pw
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Email.FallbackPassword
}
