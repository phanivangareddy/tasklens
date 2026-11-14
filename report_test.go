package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func testDirs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func snapAt(at time.Time, cpu, memPct float64, procs ...Proc) Snapshot {
	return Snapshot{Time: at.UnixMilli(), Procs: procs, System: System{Host: "test", CPU: cpu,
		MemTotal: 100 * gb, MemUsed: uint64(memPct * gb), DiskTot: 1000 * gb, DiskFree: 500 * gb}}
}

func TestHistoryKeepsFiveMinuteSummaries(t *testing.T) {
	testDirs(t)
	r := newRecorder()
	start := time.Now().Add(-20 * time.Minute).Truncate(bucketSize)
	for i := 0; i < 40; i++ { // 10 minutes of samples, every 15 s
		at := start.Add(time.Duration(i) * 15 * time.Second)
		r.add(snapAt(at, 50, 60,
			Proc{Name: "render", CPU: 40, Mem: 30 * gb},
			Proc{Name: "Chrome Helper", CPU: 3, Mem: 1 * gb}, Proc{Name: "Chrome Helper", CPU: 2, Mem: 1 * gb}))
	}
	r.flush()
	got := r.slots(start.Add(-time.Minute))
	if len(got) != 2 {
		t.Fatalf("want 2 five-minute slots, got %d", len(got))
	}
	s := got[0]
	if s.N != 20 || s.CPU != 50 || s.Mem != 60 {
		t.Fatalf("bad slot %+v", s)
	}
	var chrome *appSlot
	for i := range s.Top {
		if s.Top[i].Name == "Chrome Helper" {
			chrome = &s.Top[i]
		}
	}
	if chrome == nil || chrome.Mem != 2*gb || chrome.CPU != 5 {
		t.Fatalf("helpers with the same name should be added together: %+v", s.Top)
	}
}

func TestHistoryNeverKeepsMoreThanTwoWeeks(t *testing.T) {
	testDirs(t)
	r := newRecorder()
	os.MkdirAll(r.dir, 0o700)
	old := r.dayFile(time.Now().AddDate(0, 0, -keepDays-1))
	recent := r.dayFile(time.Now().AddDate(0, 0, -3))
	os.WriteFile(old, []byte("{}\n"), 0o600)
	os.WriteFile(recent, []byte("{}\n"), 0o600)
	r.prune(time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Fatal("a day older than two weeks should be deleted")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("recent days must be kept")
	}
}

func TestRestartReasons(t *testing.T) {
	testDirs(t)
	now := time.Now().Truncate(time.Minute)
	b1, b2, b3 := now.Add(-72*time.Hour), now.Add(-48*time.Hour), now.Add(-24*time.Hour)
	in := &insights{at: time.Now(), sig: signals{
		knowsShutdowns: true,
		boots:          []time.Time{b1, b2, b3},
		shutdowns:      []time.Time{b1.Add(-2 * time.Minute)},                                   // b1: clean
		crashes:        []sysEvent{{t: b2.Add(20 * time.Second), text: "macOS crashed (test)"}}, // b2: panic report written at boot
	}} // b3: no record at all, and memory was full just before
	var slots []slot
	for i := 6; i >= 1; i-- {
		slots = append(slots, slot{T: b3.Add(-time.Duration(i) * bucketSize).UnixMilli(), N: 20, MemMax: 99, CPUMax: 100,
			Top: []appSlot{{Name: "python3.11", Mem: 40 * gb}}})
	}
	rs := in.restarts(now.AddDate(0, 0, -14), slots)
	kind := map[int64]Restart{}
	for _, r := range rs {
		kind[r.Boot] = r
	}
	if k := kind[b1.UnixMilli()].Kind; k != "normal" {
		t.Errorf("clean shut-down should be normal, got %q", k)
	}
	if r := kind[b2.UnixMilli()]; r.Kind != "crash" || r.Reason != "macOS crashed (test)" {
		t.Errorf("panic should be a crash, got %+v", r)
	}
	r3 := kind[b3.UnixMilli()]
	if r3.Kind != "unexpected" || !strings.Contains(r3.Reason, "froze") || !strings.Contains(r3.Before, "python3.11 40 GB") {
		t.Errorf("unexpected restart after full memory should say so, got %+v", r3)
	}
	if r3.Down == 0 || time.UnixMilli(r3.Down).After(b3) {
		t.Errorf("down time should come from the last history before the restart: %+v", r3)
	}
}

func TestSlowSpansAndSummary(t *testing.T) {
	base := time.Now().Add(-3 * time.Hour).Truncate(bucketSize)
	var slots []slot
	for i := 0; i < 36; i++ { // 3 hours; memory over 90% for the middle hour
		s := slot{T: base.Add(time.Duration(i) * bucketSize).UnixMilli(), N: 20, CPU: 20, CPUMax: 40, Mem: 50, MemMax: 55,
			Top: []appSlot{{Name: "Safari", CPU: 5, Mem: 2 * gb}}}
		if i >= 12 && i < 24 {
			s.Mem, s.MemMax = 93, 96
			s.Top = append(s.Top, appSlot{Name: "render", CPU: 15, Mem: 38 * gb})
		}
		slots = append(slots, s)
	}
	spans := slowSpans(slots)
	if len(spans) != 1 || spans[0].Kind != "memory" || spans[0].App != "render" || spans[0].To-spans[0].From != int64(time.Hour/time.Millisecond) {
		t.Fatalf("want one hour-long memory span caused by render, got %+v", spans)
	}
	r := Report{Days: 1, Hours: 3, Slow: spans, Apps: appTotals(slots),
		Problems: []Problem{{Kind: "memory", App: "render", Count: 3}}}
	text := strings.Join(summary(r), "\n")
	for _, want := range []string{"No restarts", "Memory ran out 3 times", "render was using 38 GB", "Most memory: render"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary is missing %q:\n%s", want, text)
		}
	}
}

func TestGroupProblems(t *testing.T) {
	h := int64(3600_000)
	ps := []Problem{{Time: 10 * h, Kind: "memory", App: "x"}, {Time: 10*h + 600_000, Kind: "memory", App: "x"},
		{Time: 10*h + 1200_000, Kind: "memory", App: "x"}, {Time: 15 * h, Kind: "memory", App: "x"}}
	g := groupProblems(ps, time.UnixMilli(0))
	if len(g) != 2 || g[1].Count != 3 || g[1].Last != 10*h+1200_000 {
		t.Fatalf("three in a row should be grouped: %+v", g)
	}
}

func TestEmailText(t *testing.T) {
	r := Report{Host: "home-mac", OS: "macOS 26", Days: 7, From: 1, To: 2,
		Summary:  []string{"Restarted 1 time."},
		Restarts: []Restart{{Boot: 3, Kind: "crash", Reason: "macOS crashed", Before: "In the 30 minutes before: memory up to 99%."}},
		Slow:     []slowSpan{{From: 1, To: 600_000, Kind: "memory", Peak: 98, App: "render", AppUse: "38 GB"}},
		Apps:     []appTotal{{Name: "render", Share: 60, PeakMem: 38 * gb}},
		Problems: []Problem{{Time: 5, Last: 6, Count: 3, Kind: "memory", Title: "Memory ran out"}}}
	txt := r.text()
	for _, want := range []string{"home-mac", "RESTARTS", "macOS crashed", "memory up to 99%", "SLOW TIMES", "render using 38 GB",
		"APPS THAT USED THE MOST", "PROBLEMS", "(3 times"} {
		if !strings.Contains(txt, want) {
			t.Errorf("email is missing %q:\n%s", want, txt)
		}
	}
}

func TestRestartReasonSurvivesSystemCleanup(t *testing.T) {
	testDirs(t)
	boot := time.Now().Add(-48 * time.Hour).Truncate(time.Minute)
	in := &insights{at: time.Now(), sig: signals{knowsShutdowns: true, boots: []time.Time{boot},
		crashes: []sysEvent{{t: boot.Add(30 * time.Second), text: "macOS crashed (test)"}}}}
	in.restarts(time.Now().AddDate(0, 0, -14), nil)
	in.sig.crashes = nil // macOS deleted its crash report
	for _, r := range in.restarts(time.Now().AddDate(0, 0, -14), nil) {
		if r.Boot == boot.UnixMilli() && (r.Kind != "crash" || r.Reason != "macOS crashed (test)") {
			t.Fatalf("the crash should still be reported after the system deletes its record: %+v", r)
		}
	}
}
