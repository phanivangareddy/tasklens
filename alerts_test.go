package main

import (
	"bufio"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const gb = 1 << 30

// fakeCollector feeds the monitor a scripted sequence of samples.
func newTestMonitor(t *testing.T) (*monitor, *[]Alert, *collector) {
	t.Setenv("HOME", t.TempDir()) // keep settings/state out of the real config folder
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := &collector{statics: map[int64]static{}, prev: map[int32]prevCPU{}, paused: map[int32]bool{}}
	st := &settingsStore{s: defaultSettings()}
	m := &monitor{col: c, settings: st, since: map[string]time.Time{}, lastFired: map[string]time.Time{}}
	var got []Alert
	m.email = func(a Alert) string { got = append(got, a); return "sent" }
	st.s.Email.Enabled = true
	return m, &got, c
}

func feed(m *monitor, c *collector, at time.Time, memUsed, swap uint64, procs []Proc) {
	snap := Snapshot{Time: at.UnixMilli(), Procs: procs, System: System{
		Host: "test", MemTotal: 48 * gb, MemUsed: memUsed, SwapUsed: swap, CPU: 10, DiskTot: 1000 * gb, DiskFree: 500 * gb}}
	c.mu.Lock()
	c.lastSnap, c.lastAt = snap, at
	c.mu.Unlock()
	// memForecast looks at "the last 3 minutes" of real time, so shift history to now.
	m.check(snap)
}

func TestMemoryForecastNamesTheGrowingApp(t *testing.T) {
	m, got, c := newTestMonitor(t)
	start := time.Now().Add(-2 * time.Minute)
	for i := 0; i <= 8; i++ { // render grows 1.5 GB every 15 s; memory 36 → 48 GB
		at := start.Add(time.Duration(i) * 15 * time.Second)
		render := uint64(20*gb) + uint64(i)*uint64(1536)<<20
		feed(m, c, at, 24*gb+render-uint64(8*gb), 0, []Proc{
			{PID: 4242, Name: "python3.11", Mem: render, Started: start.Add(-time.Hour).UnixMilli()},
			{PID: 77, Name: "WindowServer", Mem: 1 * gb, System: true},
		})
	}
	var fc *Alert
	for i := range *got {
		if (*got)[i].Rule == "memForecast" {
			fc = &(*got)[i]
		}
	}
	if fc == nil {
		t.Fatalf("expected an early-warning alert, got %+v", *got)
	}
	if !fc.Critical || fc.PID != 4242 || !strings.Contains(fc.Body, "python3.11") {
		t.Fatalf("early warning should be urgent and name python3.11: %+v", *fc)
	}
	t.Logf("fired: %s / %s", fc.Title, fc.Body)
}

func TestCooldownStopsRepeats(t *testing.T) {
	m, got, c := newTestMonitor(t)
	now := time.Now()
	for i := 0; i < 20; i++ { // memory stuck at 95% for 5 minutes
		feed(m, c, now.Add(time.Duration(i)*15*time.Second), uint64(46694)<<20, 0, []Proc{{PID: 9, Name: "app", Mem: 2 * gb}})
	}
	n := 0
	for _, a := range *got {
		if a.Rule == "memHigh" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("memHigh should fire once within the cooldown, fired %d times", n)
	}
}

func TestBriefSpikeIsIgnored(t *testing.T) {
	m, got, c := newTestMonitor(t)
	now := time.Now()
	feed(m, c, now, uint64(46*gb), 0, nil)                     // 96% for one sample...
	feed(m, c, now.Add(15*time.Second), uint64(30*gb), 0, nil) // ...then back down
	feed(m, c, now.Add(30*time.Second), uint64(46*gb), 0, nil)
	for _, a := range *got {
		if a.Rule == "memHigh" {
			t.Fatalf("a spike shorter than a minute should not alert: %+v", a)
		}
	}
}

func TestSystemProcessIsNotOfferedForPausing(t *testing.T) {
	m, got, c := newTestMonitor(t)
	now := time.Now()
	for i := 0; i < 6; i++ {
		feed(m, c, now.Add(time.Duration(i)*15*time.Second), 30*gb, 0,
			[]Proc{{PID: 1234, Name: "kernel_task", Mem: 30 * gb, System: true}})
	}
	for _, a := range *got {
		if a.Rule == "appHigh" && a.PID != 0 {
			t.Fatalf("system processes must not get a Pause button: %+v", a)
		}
	}
}

func TestPresetsAreValid(t *testing.T) {
	for name, p := range presets {
		if err := validLimits(p); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSettingsRejectBadEmail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := &settingsStore{s: defaultSettings()}
	s := defaultSettings()
	s.Email.Recipients = []Recipient{{Email: "not-an-email"}}
	if err := st.save(s); err == nil {
		t.Fatal("expected a bad address to be rejected")
	}
	s.Email.Recipients = []Recipient{{Email: " Mom@Example.com "}, {Email: "mom@example.com"}}
	if err := st.save(s); err != nil {
		t.Fatal(err)
	}
	if got := st.get().Email.Recipients; len(got) != 1 || got[0].Email != "Mom@Example.com" {
		t.Fatalf("expected one trimmed, de-duplicated recipient, got %+v", got)
	}
	os.Remove(configDir())
}

// A mail server that doesn't offer encryption must never receive the password.
func TestRefusesUnencryptedServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotAuth := make(chan bool, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		conn.Write([]byte("220 fake ESMTP\r\n"))
		auth := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				conn.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "AUTH"):
				auth = true
				conn.Write([]byte("235 ok\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				conn.Write([]byte("221 bye\r\n"))
			default:
				conn.Write([]byte("250 ok\r\n"))
			}
		}
		gotAuth <- auth
	}()
	st := &settingsStore{s: defaultSettings()}
	st.s.Email.FallbackPassword = "secret"
	ml := &mailer{settings: st}
	port := ln.Addr().(*net.TCPAddr).Port
	err = ml.send(EmailSettings{Host: "127.0.0.1", Port: port, Username: "nobody-test@example.invalid"}, []string{"a@example.com"}, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("expected a refusal to send without encryption, got %v", err)
	}
	if <-gotAuth {
		t.Fatal("the password was sent to an unencrypted server")
	}
}
