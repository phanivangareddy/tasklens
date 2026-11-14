package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const checkEvery = 15 * time.Second

type Alert struct {
	Time     int64  `json:"time"`
	Rule     string `json:"rule"`
	Critical bool   `json:"critical"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	PID      int32  `json:"pid,omitempty"` // the app to offer pausing, if it's the user's own
	App      string `json:"app,omitempty"`
	Emailed  string `json:"emailed,omitempty"` // "" | "sent" | error text
}

type point struct {
	t        time.Time
	memUsed  uint64
	swapUsed uint64
	cpu      float64
	procs    map[int32]uint64 // memory of the biggest apps, to see who is growing
}

type diskPoint struct {
	T    int64  `json:"t"`
	Free uint64 `json:"free"`
}

type monitor struct {
	col      *collector
	settings *settingsStore
	notify   func(Alert)        // pop-up on this computer
	email    func(Alert) string // returns "sent" or why not
	record   func(Snapshot)     // the two-week history

	mu         sync.Mutex
	hist       []point
	since      map[string]time.Time // when a condition started being true
	lastFired  map[string]time.Time
	log        []Alert
	disk       []diskPoint // hourly free-space samples, two weeks
	mutedUntil time.Time
}

func newMonitor(c *collector, s *settingsStore) *monitor {
	m := &monitor{col: c, settings: s, since: map[string]time.Time{}, lastFired: map[string]time.Time{}}
	m.loadState()
	return m
}

func statePath() string { return filepath.Join(configDir(), "state.json") }

func (m *monitor) loadState() {
	var st struct {
		Disk []diskPoint `json:"disk"`
		Log  []Alert     `json:"log"`
	}
	if raw, err := os.ReadFile(statePath()); err == nil && json.Unmarshal(raw, &st) == nil {
		m.disk, m.log = st.Disk, st.Log
	}
}

func (m *monitor) saveState() {
	m.mu.Lock()
	raw, _ := json.Marshal(map[string]any{"disk": m.disk, "log": m.log})
	m.mu.Unlock()
	os.MkdirAll(configDir(), 0o700)
	os.WriteFile(statePath(), raw, 0o600)
}

func (m *monitor) run(watching func() bool) {
	for {
		time.Sleep(checkEvery)
		if !watching() {
			continue
		}
		if m.col.age() > 10*time.Second { // the dashboard isn't open, so nobody else is sampling
			m.col.sample()
		}
		snap := m.col.last()
		if m.record != nil && m.settings.get().History {
			m.record(snap)
		}
		m.check(snap)
	}
}

func (m *monitor) mute(d time.Duration) {
	m.mu.Lock()
	m.mutedUntil = time.Now().Add(d)
	m.mu.Unlock()
}

func (m *monitor) muted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Now().Before(m.mutedUntil)
}

func (m *monitor) recent() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Alert(nil), m.log...)
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out
}

// check runs every rule against the newest sample.
func (m *monitor) check(snap Snapshot) {
	s := snap.System
	if s.MemTotal == 0 {
		return
	}
	set := m.settings.get()
	lim := set.Limits
	now := time.Unix(0, snap.Time*int64(time.Millisecond))

	top := map[int32]uint64{}
	for i, p := range snap.Procs { // already sorted by memory
		if i == 40 {
			break
		}
		top[p.PID] = p.Mem
	}
	m.mu.Lock()
	m.hist = append(m.hist, point{now, s.MemUsed, s.SwapUsed, s.CPU, top})
	for len(m.hist) > 0 && now.Sub(m.hist[0].t) > 15*time.Minute {
		m.hist = m.hist[1:]
	}
	if n := len(m.disk); s.DiskTot > 0 && (n == 0 || now.UnixMilli()-m.disk[n-1].T >= int64(time.Hour/time.Millisecond)) {
		m.disk = append(m.disk, diskPoint{now.UnixMilli(), s.DiskFree})
		if len(m.disk) > 24*14 {
			m.disk = m.disk[len(m.disk)-24*14:]
		}
		defer m.saveState()
	}
	m.mu.Unlock()

	memPct := float64(s.MemUsed) / float64(s.MemTotal) * 100
	var biggest *Proc
	if len(snap.Procs) > 0 {
		biggest = &snap.Procs[0]
	}
	mine := func(p *Proc) int32 {
		if p != nil && !p.System {
			return p.PID
		}
		return 0
	}
	topList := func(n int) string {
		var parts []string
		for i := 0; i < n && i < len(snap.Procs); i++ {
			parts = append(parts, fmt.Sprintf("%s %s", snap.Procs[i].Name, human(snap.Procs[i].Mem)))
		}
		return strings.Join(parts, ", ")
	}

	// 1. Memory will run out soon: the early warning, before things freeze.
	if set.Rules["memForecast"] && memPct >= 70 {
		if eta, grower, grew := m.memForecast(s.MemTotal); eta > 0 && eta <= lim.ForecastMin*60 {
			a := Alert{Rule: "memForecast", Critical: true,
				Title: fmt.Sprintf("Memory will run out in about %s", minutes(eta))}
			if grower != nil {
				a.Body = fmt.Sprintf("%s has grown by %s in the last few minutes and now uses %s. Pause or close it before the computer slows down.",
					grower.Name, human(grew), human(grower.Mem))
				a.PID, a.App = mine(grower), grower.Name
			} else {
				a.Body = fmt.Sprintf("Memory use is climbing fast (now %.0f%%). Biggest apps: %s.", memPct, topList(3))
			}
			m.fire(a, "", now, lim, set)
		}
	}
	// 2. Memory is full right now.
	if m.held("memHigh", set.Rules["memHigh"] && memPct >= lim.MemPct, now, time.Minute) {
		a := Alert{Rule: "memHigh", Critical: true, Title: fmt.Sprintf("Memory is %.0f%% full", memPct),
			Body: fmt.Sprintf("Everything will feel slow until some memory is freed. Biggest apps: %s.", topList(3))}
		if biggest != nil {
			a.PID, a.App = mine(biggest), biggest.Name
		}
		m.fire(a, "", now, lim, set)
	}
	// 3. One app is taking a big share of memory.
	for i := 0; i < 3 && i < len(snap.Procs); i++ {
		p := &snap.Procs[i]
		share := float64(p.Mem) / float64(s.MemTotal) * 100
		key := fmt.Sprintf("appHigh:%d", p.PID)
		if m.held(key, set.Rules["appHigh"] && share >= lim.AppPct, now, time.Minute) {
			body := fmt.Sprintf("That's %.0f%% of this computer's memory.", share)
			if p.Started > 0 {
				body += " It has been running for " + dur(now.Sub(time.UnixMilli(p.Started))) + "."
			}
			m.fire(Alert{Rule: "appHigh", Critical: share >= 90,
				Title: fmt.Sprintf("%s is using %s of memory", p.Name, human(p.Mem)),
				Body:  body,
				PID:   mine(p), App: p.Name}, key, now, lim, set)
		}
	}
	// 4. Lots of memory pushed out to disk.
	swapPct := float64(s.SwapUsed) / float64(s.MemTotal) * 100
	if m.held("swapHigh", set.Rules["swapHigh"] && swapPct >= lim.SwapPct, now, time.Minute) {
		m.fire(Alert{Rule: "swapHigh", Title: fmt.Sprintf("%s of memory has been moved to disk", human(s.SwapUsed)),
			Body: fmt.Sprintf("The computer ran short of memory, so it is using the disk instead, which is much slower. Closing big apps or restarting clears it. Biggest apps: %s.", topList(3))},
			"", now, lim, set)
	}
	// 5. Processor flat out for a while.
	if m.held("cpuHigh", set.Rules["cpuHigh"] && s.CPU >= lim.CPUPct, now, time.Duration(lim.CPUMin*float64(time.Minute))) {
		byCPU := append([]Proc(nil), snap.Procs...)
		sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].CPU > byCPU[j].CPU })
		var parts []string
		for i := 0; i < 3 && i < len(byCPU); i++ {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", byCPU[i].Name, byCPU[i].CPU))
		}
		a := Alert{Rule: "cpuHigh", Title: fmt.Sprintf("The processor has been %.0f%% busy for %.0f minutes", s.CPU, lim.CPUMin),
			Body: "Top users: " + strings.Join(parts, ", ") + "."}
		if len(byCPU) > 0 {
			a.PID, a.App = mine(&byCPU[0]), byCPU[0].Name
		}
		m.fire(a, "", now, lim, set)
	}
	// 6. Disk nearly full.
	if s.DiskTot > 0 {
		freePct := float64(s.DiskFree) / float64(s.DiskTot) * 100
		if set.Rules["diskLow"] && freePct <= lim.DiskFreePct {
			m.fire(Alert{Rule: "diskLow", Critical: freePct <= lim.DiskFreePct/2,
				Title: fmt.Sprintf("Only %s of disk space left", human(s.DiskFree)),
				Body:  "Open TaskLens → Disk to see which folders and files are using the space."}, "", now, lim, set)
		} else if days := m.diskForecast(); set.Rules["diskForecast"] && days > 0 && days <= lim.DiskDays {
			m.fire(Alert{Rule: "diskForecast", Title: fmt.Sprintf("The disk will be full in about %.0f days", days),
				Body: fmt.Sprintf("%s is free, but it has been shrinking steadily. Open TaskLens → Disk to see what is growing.", human(s.DiskFree))},
				"", now, lim, set)
		}
	}
}

// held reports whether cond has been true continuously for at least d.
func (m *monitor) held(key string, cond bool, now time.Time, d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !cond {
		delete(m.since, key)
		return false
	}
	t, ok := m.since[key]
	if !ok {
		m.since[key] = now
		return d <= 0
	}
	return now.Sub(t) >= d
}

// memForecast fits a line to memory+swap over the last 3 minutes and returns
// the seconds until memory is used up, plus the app that grew the most.
func (m *monitor) memForecast(total uint64) (eta float64, grower *Proc, grew uint64) {
	m.mu.Lock()
	var pts []point
	for _, p := range m.hist {
		if time.Since(p.t) <= 3*time.Minute+checkEvery {
			pts = append(pts, p)
		}
	}
	m.mu.Unlock()
	if len(pts) < 5 {
		return 0, nil, 0
	}
	t0 := pts[0].t
	var sx, sy, sxx, sxy float64
	for _, p := range pts {
		x := p.t.Sub(t0).Seconds()
		y := float64(p.memUsed)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	n := float64(len(pts))
	slope := (n*sxy - sx*sy) / (n*sxx - sx*sx) // bytes per second
	last := pts[len(pts)-1]
	swapSlope := (float64(last.swapUsed) - float64(pts[0].swapUsed)) / last.t.Sub(t0).Seconds()
	const minRate = 15 << 20 // ignore anything slower than 15 MB/s (about 1 GB a minute)
	switch {
	case slope >= minRate:
		room := float64(total)*0.97 - float64(last.memUsed)
		eta = room / slope
		if eta < 1 {
			eta = 1
		}
	case swapSlope >= minRate && float64(last.memUsed) >= float64(total)*0.85:
		eta = 1 // memory is already full and spilling to disk
	default:
		return 0, nil, 0
	}
	snap := m.col.last()
	var best uint64
	for i := range snap.Procs {
		p := &snap.Procs[i]
		if old, ok := pts[0].procs[p.PID]; ok && p.Mem > old && p.Mem-old > best {
			best, grower = p.Mem-old, p
		} else if !ok && p.Mem > best && p.Started >= t0.UnixMilli() {
			best, grower = p.Mem, p // started during the window
		}
	}
	if best < 512<<20 {
		grower = nil
	}
	return eta, grower, best
}

// diskForecast uses up to two weeks of hourly samples; it needs at least half a
// day of data and a steady decline before it predicts anything.
func (m *monitor) diskForecast() float64 {
	m.mu.Lock()
	pts := append([]diskPoint(nil), m.disk...)
	m.mu.Unlock()
	if len(pts) < 12 {
		return 0
	}
	first, last := pts[0], pts[len(pts)-1]
	hours := float64(last.T-first.T) / 3.6e6
	if hours < 12 || last.Free >= first.Free {
		return 0
	}
	perDay := float64(first.Free-last.Free) / hours * 24
	if perDay < 1<<30 { // less than 1 GB a day is noise
		return 0
	}
	return float64(last.Free) / perDay
}

func (m *monitor) fire(a Alert, key string, now time.Time, lim Thresholds, set Settings) {
	if key == "" {
		key = a.Rule
	}
	m.mu.Lock()
	if t, ok := m.lastFired[key]; ok && now.Sub(t) < time.Duration(lim.CooldownMin*float64(time.Minute)) {
		m.mu.Unlock()
		return
	}
	m.lastFired[key] = now
	muted := time.Now().Before(m.mutedUntil)
	m.mu.Unlock()

	a.Time = now.UnixMilli()
	log.Printf("alert: %s: %s", a.Title, a.Body)
	if !muted && set.Popups && m.notify != nil {
		go m.notify(a)
	}
	if !muted && set.Email.Enabled && m.email != nil {
		a.Emailed = m.email(a)
	}
	m.mu.Lock()
	m.log = append(m.log, a)
	if len(m.log) > 100 {
		m.log = m.log[len(m.log)-100:]
	}
	m.mu.Unlock()
	m.saveState()
}

func human(n uint64) string {
	const gb = 1 << 30
	switch {
	case n >= 10*gb:
		return fmt.Sprintf("%.0f GB", float64(n)/gb)
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	default:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
}

func minutes(sec float64) string {
	if sec < 90 {
		return "a minute"
	}
	return fmt.Sprintf("%.0f minutes", sec/60)
}

func dur(d time.Duration) string {
	h := int(d.Hours())
	switch {
	case h >= 48:
		return fmt.Sprintf("%d days", h/24)
	case h >= 1:
		return fmt.Sprintf("%dh %dm", h, int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
}
