package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseLast(t *testing.T) {
	now := time.Date(2026, 1, 3, 10, 0, 0, 0, time.Local)
	kind, at, ok := parseLast("reboot time                                Thu Dec 31 11:48", now)
	if !ok || kind != "reboot" || at.Year() != 2025 || at.Hour() != 11 {
		t.Fatalf("got %v %v %v", kind, at, ok)
	}
	if _, _, ok := parseLast("wtmp begins Sat Jul 25 03:09:24 EDT 2026", now); ok {
		t.Fatal("header line should be ignored")
	}
}

func TestDiagReports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	var sig signals
	mod := time.Now()
	readReport(write("JetsamEvent-2026-11-12-063707.ips",
		`{"bug_type":"298","timestamp":"2026-11-12 06:37:07.00 -0500"}
{
  "largestProcess" : "python3.11",
  "processes" : [
  {"name":"a","reason":"vm-compressor-space-shortage"},
  {"name":"z","reason":"idle-exit"},
  {"name":"b","reason":"vm-compressor-space-shortage"},
  {"name":"c"}
  ]
}`), "JetsamEvent-2026-11-12-063707.ips", mod, &sig)
	readReport(write("panic-full-2026-11-05-122433.0002.panic",
		`{"bug_type":"210","timestamp":"2026-11-05 12:24:33.00 -0500"}
{
  "panicString" : "panic(cpu 0 caller 0x1): watchdog timeout: no checkins from watchdogd in 94 seconds (1790 total)\nDebugger"
}`), "panic-full-2026-11-05-122433.0002.panic", mod, &sig)
	readReport(write("java_2026-11-09-125018_Mac.diag", "Date/Time:        2026-11-09 12:43:37 -0500\nEvent:            disk writes\nWrites:           34.36 GB of file backed memory dirtied over 401 seconds (85.66 MB per second average)\n"),
		"java_2026-11-09-125018_Mac.diag", mod, &sig)
	readReport(write("node_2026-11-09-194945_Mac.diag", "Event:            disk writes\nWrites:           2147.48 MB of file backed memory dirtied over 10959 seconds\n"),
		"node_2026-11-09-194945_Mac.diag", mod, &sig)
	readReport(write("makensis-2026-11-10-141632.ips", `{"app_name":"makensis","timestamp":"2026-11-10 14:16:32.00 -0500","bug_type":"309"}`+"\n{}"),
		"makensis-2026-11-10-141632.ips", mod, &sig)

	if len(sig.crashes) != 1 || !strings.Contains(sig.crashes[0].text, "94 seconds") {
		t.Fatalf("panic not understood: %+v", sig.crashes)
	}
	kinds := map[string]Problem{}
	for _, p := range sig.problems {
		kinds[p.Kind] = p
	}
	if p := kinds["memory"]; p.App != "python3.11" || !strings.Contains(p.Title, "closed 2 background apps") {
		t.Errorf("memory report: %+v", p)
	}
	if p := kinds["disk"]; p.App != "java" || !strings.Contains(p.Title, "34 GB") {
		t.Errorf("heavy disk writer: %+v", p)
	}
	if p := kinds["crash"]; p.App != "makensis" {
		t.Errorf("crash: %+v", p)
	}
	if len(sig.problems) != 4 { // node's slow trickle of writes is normal and left out
		t.Errorf("want 4 problems, got %d: %+v", len(sig.problems), sig.problems)
	}
}

// Run with TASKLENS_REAL=1 to print what this Mac's own records say.
func TestRealMac(t *testing.T) {
	if os.Getenv("TASKLENS_REAL") == "" {
		t.Skip()
	}
	start := time.Now()
	sig := readSignals(time.Now().AddDate(0, 0, -keepDays))
	t.Logf("read in %s: %d boots, %d shutdowns, %d crashes, %d problems", time.Since(start), len(sig.boots), len(sig.shutdowns), len(sig.crashes), len(sig.problems))
	in := &insights{at: time.Now(), sig: sig}
	for _, r := range in.restarts(time.Now().AddDate(0, 0, -keepDays), nil) {
		t.Logf("restart %s [%s] %s %s", time.UnixMilli(r.Boot).Format(time.Stamp), r.Kind, r.Reason, r.Note)
	}
	for _, p := range groupProblems(sig.problems, time.Now().AddDate(0, 0, -keepDays)) {
		t.Logf("problem %s [%s] %s ×%d %s", time.UnixMilli(p.Time).Format(time.Stamp), p.Kind, p.Title, p.Count, p.Detail)
	}
}
