//go:build !darwin && !windows

package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Linux: the systemd journal lists every boot with its first and last entry,
// and the end of each boot shows whether it shut down cleanly. The kernel log
// records apps killed because memory ran out; coredumpctl lists app crashes.
// Reading the journal may need the user to be in the "adm" or
// "systemd-journal" group; without it the report says the reason is unknown.
func readSignals(since time.Time) signals {
	var sig signals
	out, err := runTool(10*time.Second, sysTool("journalctl"), "--list-boots", "--no-pager")
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			id, first, last, ok := parseBoot(line)
			if !ok || last.Before(since) {
				continue
			}
			if !first.Before(since) {
				sig.boots = append(sig.boots, first)
			}
			if strings.HasPrefix(strings.TrimSpace(line), "0 ") { // the current boot
				continue
			}
			tail, err := runTool(10*time.Second, sysTool("journalctl"), "-b", id, "-n", "60", "-o", "cat", "--no-pager", "-q")
			if err != nil || len(tail) == 0 {
				continue
			}
			sig.knowsShutdowns = true
			if cleanEnd(string(tail)) {
				sig.shutdowns = append(sig.shutdowns, last)
			}
		}
	}
	if out, err := runTool(15*time.Second, sysTool("journalctl"), "_TRANSPORT=kernel", "--since", "14 days ago", "-o", "short-unix", "--no-pager", "-q", "-g", "Out of memory: Killed process"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if t, name, ok := oomLine(line); ok {
				sig.problems = append(sig.problems, Problem{Time: t.UnixMilli(), Kind: "memory", App: name,
					Title: "Memory ran out, so Linux closed " + name})
			}
		}
	}
	if out, err := runTool(15*time.Second, sysTool("coredumpctl"), "list", "--since", "14 days ago", "--no-pager", "--no-legend", "-q"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if m := coreRe.FindStringSubmatch(line); m != nil {
				t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
				if err != nil {
					continue
				}
				app := oneLine(m[2][strings.LastIndex(m[2], "/")+1:])
				sig.problems = append(sig.problems, Problem{Time: t.UnixMilli(), Kind: "crash", App: app, Title: app + " crashed"})
			}
		}
	}
	return sig
}

var bootRe = regexp.MustCompile(`^\s*(-?\d+)\s+([0-9a-f]{32})\s+\w{3} (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) \S+\s*[—–-]?\s*\w{3} (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)`)

func parseBoot(line string) (id string, first, last time.Time, ok bool) {
	m := bootRe.FindStringSubmatch(line)
	if m == nil {
		return "", first, last, false
	}
	f, e1 := time.ParseInLocation("2006-01-02 15:04:05", m[3], time.Local)
	l, e2 := time.ParseInLocation("2006-01-02 15:04:05", m[4], time.Local)
	return m[2], f, l, e1 == nil && e2 == nil
}

func cleanEnd(tail string) bool {
	for _, k := range []string{"Reached target System Power Off", "Reached target System Reboot", "Reached target Power-Off",
		"Reached target Reboot", "Reached target Shutdown", "Reached target System Shutdown", "systemd-shutdown", "Shutting down.", "System is rebooting", "System is powering down"} {
		if strings.Contains(tail, k) {
			return true
		}
	}
	return false
}

var oomRe = regexp.MustCompile(`^(\d+)(?:\.\d+)?\s.*Out of memory: Killed process \d+ \(([^)]+)\)`)

func oomLine(line string) (time.Time, string, bool) {
	m := oomRe.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}, "", false
	}
	sec, _ := strconv.ParseInt(m[1], 10, 64)
	return time.Unix(sec, 0), oneLine(m[2]), true
}

var coreRe = regexp.MustCompile(`^\w{3} (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) \S+\s+\d+\s+\d+\s+\d+\s+\S+\s+\S+\s+(\S+)`)
