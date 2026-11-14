package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// macOS: `last` lists every boot and clean shut-down; DiagnosticReports holds
// kernel panics, app crashes and hangs, and "Jetsam" reports written when
// memory ran out and macOS closed apps. Only file names and the first few
// kilobytes are read, so this stays cheap.
func readSignals(since time.Time) signals {
	var sig signals
	if out, err := runTool(10*time.Second, "/usr/bin/last", "reboot", "shutdown"); err == nil {
		sig.knowsShutdowns = true
		for _, line := range strings.Split(string(out), "\n") {
			kind, t, ok := parseLast(line, time.Now())
			if !ok || t.Before(since) {
				continue
			}
			if kind == "reboot" {
				sig.boots = append(sig.boots, t)
			} else {
				sig.shutdowns = append(sig.shutdowns, t)
			}
		}
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Library/Logs/DiagnosticReports", filepath.Join(home, "Library/Logs/DiagnosticReports")} {
		readReportsIn(dir, since, &sig)
	}
	return sig
}

func readReportsIn(dir string, since time.Time, sig *signals) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		// Only ordinary files: a pipe or device planted here would make
		// reading wait forever, and a link could point anywhere.
		name := e.Name()
		if !e.Type().IsRegular() || (strings.HasPrefix(name, ".") && name != ".contents.panic") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		readReport(filepath.Join(dir, e.Name()), e.Name(), info.ModTime(), sig)
	}
}

var lastRe = regexp.MustCompile(`^(reboot|shutdown)\b.*?([A-Z][a-z]{2} [A-Z][a-z]{2} [ \d]\d \d\d:\d\d)\s*$`)

// parseLast reads lines like "reboot time   Thu Nov 12 11:48". `last` leaves the
// year out, so the most recent matching date is used.
func parseLast(line string, now time.Time) (string, time.Time, bool) {
	m := lastRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", time.Time{}, false
	}
	for y := now.Year(); y >= now.Year()-1; y-- {
		t, err := time.ParseInLocation("2006 Mon Jan _2 15:04", fmt.Sprintf("%d %s", y, m[2]), time.Local)
		if err == nil && !t.After(now.Add(24*time.Hour)) {
			return m[1], t, true
		}
	}
	return "", time.Time{}, false
}

type reportHead struct {
	AppName   string `json:"app_name"`
	Name      string `json:"name"`
	Timestamp string `json:"timestamp"`
	BugType   string `json:"bug_type"`
}

var fileTimeRe = regexp.MustCompile(`[-_](\d{4}-\d\d-\d\d-\d{6})`)

func readReport(path, name string, mod time.Time, sig *signals) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 16<<10)
	first, _ := br.ReadSlice('\n')

	app := name
	t := mod
	if m := fileTimeRe.FindStringSubmatchIndex(name); m != nil {
		app = name[:m[0]]
		if ft, err := time.ParseInLocation("2006-01-02-150405", name[m[2]:m[3]], time.Local); err == nil {
			t = ft
		}
	}
	var head reportHead
	if json.Unmarshal(bytes.TrimSpace(first), &head) == nil {
		if ht, err := time.Parse("2006-01-02 15:04:05.00 -0700", head.Timestamp); err == nil {
			t = ht
		}
		if head.AppName != "" {
			app = head.AppName
		}
	}
	app = oneLine(app)
	ms := t.UnixMilli()
	switch {
	case name == ".contents.panic": // the summary macOS keeps after it deletes the full panic report
		raw := make([]byte, 16<<10)
		n, _ := io.ReadFull(io.MultiReader(bytes.NewReader(first), br), raw)
		var c struct {
			LogPath string `json:"log_path"`
		}
		json.Unmarshal(raw[:n], &c)
		if c.LogPath != "" {
			if _, err := os.Lstat(c.LogPath); err == nil {
				return // the full report is still there and is read instead
			}
		}
		text := panicText(bytes.NewReader(raw[:n]))
		sig.crashes = append(sig.crashes, sysEvent{t: mod, text: text})
		sig.problems = append(sig.problems, Problem{Time: mod.UnixMilli(), Kind: "system", Title: "macOS crashed and restarted", Detail: text})
	case head.BugType == "210" || strings.HasSuffix(name, ".panic"): // kernel panic
		text := panicText(br)
		sig.crashes = append(sig.crashes, sysEvent{t: t, text: text})
		sig.problems = append(sig.problems, Problem{Time: ms, Kind: "system", Title: "macOS crashed and restarted", Detail: text})
	case head.BugType == "298" || strings.HasPrefix(name, "JetsamEvent"): // memory ran out
		largest, closed, overLimit := jetsam(br)
		p := Problem{Time: ms, Kind: "memory", App: largest}
		switch {
		case closed > 0:
			p.Title = fmt.Sprintf("Memory ran out, so macOS closed %s", plural(closed, "background app"))
		case overLimit > 0:
			p.Kind, p.Title = "limit", "macOS closed an app that went over its own memory limit"
		default:
			return // routine tidy-up of idle apps
		}
		if largest = oneLine(largest); largest != "" {
			p.App, p.Detail = largest, "The biggest app at the time was "+largest+"."
		}
		sig.problems = append(sig.problems, p)
	case head.BugType == "309" || head.BugType == "109" || strings.HasSuffix(name, ".crash"):
		sig.problems = append(sig.problems, Problem{Time: ms, Kind: "crash", App: app, Title: app + " crashed"})
	case strings.HasSuffix(name, ".shutdownStall"):
		sig.slowShutdowns = append(sig.slowShutdowns, sysEvent{t: t, text: "Shutting down took longer than normal: an app was slow to close."})
	case strings.HasSuffix(name, ".hang") || strings.HasSuffix(name, ".spin") || strings.HasSuffix(name, ".diag"):
		if p, ok := diagProblem(br, app, ms, string(first)); ok {
			sig.problems = append(sig.problems, p)
		}
	}
}

var panicRe = regexp.MustCompile(`"(?:panicString|panic_string)"\s*:\s*"((?:[^"\\]|\\.)*)`)
var watchdogRe = regexp.MustCompile(`no checkins from watchdogd in (\d+) seconds`)

func panicText(r io.Reader) string {
	buf := make([]byte, 8<<10)
	n, _ := io.ReadFull(r, buf)
	m := panicRe.FindSubmatch(buf[:n])
	if m == nil {
		return "macOS crashed (a kernel panic) and restarted itself."
	}
	s := string(m[1])
	if w := watchdogRe.FindStringSubmatch(s); w != nil {
		return "macOS stopped responding for " + w[1] + " seconds and restarted itself (a watchdog timeout). This usually follows a freeze from very heavy load or a driver problem."
	}
	s = oneLine(strings.SplitN(strings.ReplaceAll(s, `\n`, "\n"), "\n", 2)[0])
	if i := strings.Index(s, "): "); i >= 0 {
		s = s[i+3:]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return "macOS crashed (a kernel panic) and restarted itself: " + s
}

var (
	largestRe = regexp.MustCompile(`"largestProcess"\s*:\s*"([^"]+)"`)
	reasonRe  = regexp.MustCompile(`"reason"\s*:\s*"([^"]+)"`)
)

// jetsam streams the report: the largest app is near the top, and each app
// macOS acted on has a "reason". Only memory-shortage reasons count; "idle-exit"
// is routine and "jettisoned" repeats earlier events. Reports can be 1–2 MB,
// so they are never loaded whole.
func jetsam(r io.Reader) (largest string, closed, overLimit int) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if largest == "" {
			if m := largestRe.FindSubmatch(line); m != nil {
				largest = string(m[1])
			}
		}
		if !bytes.Contains(line, []byte(`"reason"`)) {
			continue
		}
		if m := reasonRe.FindSubmatch(line); m != nil {
			switch reason := string(m[1]); {
			case reason == "per-process-limit":
				overLimit++
			case strings.HasPrefix(reason, "vm-") || strings.Contains(reason, "thrashing") || reason == "highwater" || reason == "zone-map-exhaustion":
				closed++
			}
		}
	}
	return largest, closed, overLimit
}

var (
	eventRe = regexp.MustCompile(`(?m)^Event:\s+(.+)$`)
	cpuRe   = regexp.MustCompile(`(?m)^CPU:\s+([\d.]+) seconds cpu time over ([\d.]+) seconds`)
	writeRe = regexp.MustCompile(`(?m)^Writes:\s+([\d.]+) (MB|GB) of file backed memory dirtied over (\d+) seconds`)
	durRe   = regexp.MustCompile(`(?m)^Duration:\s+([\d.]+)s`)
)

// diagProblem reads the text header of a resource or hang report.
func diagProblem(r io.Reader, app string, ms int64, first string) (Problem, bool) {
	buf := make([]byte, 4<<10)
	n, _ := io.ReadFull(r, buf)
	text := first + string(buf[:n])
	ev := eventRe.FindStringSubmatch(text)
	event := ""
	if ev != nil {
		event = strings.ToLower(strings.TrimSpace(ev[1]))
	}
	switch {
	case strings.Contains(event, "hang") || strings.Contains(event, "spin"):
		p := Problem{Time: ms, Kind: "hang", App: app, Title: app + " stopped responding"}
		if d := durRe.FindStringSubmatch(text); d != nil {
			if sec, _ := strconv.ParseFloat(d[1], 64); sec >= 1 {
				p.Title = fmt.Sprintf("%s stopped responding for %.0f seconds", app, sec)
			}
		}
		return p, true
	case strings.Contains(event, "cpu"):
		p := Problem{Time: ms, Kind: "cpu", App: app, Title: app + " kept the processor very busy"}
		if c := cpuRe.FindStringSubmatch(text); c != nil {
			used, _ := strconv.ParseFloat(c[1], 64)
			over, _ := strconv.ParseFloat(c[2], 64)
			if over > 0 {
				p.Detail = fmt.Sprintf("It used %s of processor time in %s.", dur(time.Duration(used)*time.Second), dur(time.Duration(over)*time.Second))
			}
		}
		return p, true
	case strings.Contains(event, "disk writes"):
		w := writeRe.FindStringSubmatch(text)
		if w == nil {
			return Problem{}, false
		}
		amt, _ := strconv.ParseFloat(w[1], 64)
		if w[2] == "GB" {
			amt *= 1024
		}
		sec, _ := strconv.ParseFloat(w[3], 64)
		if sec <= 0 || amt/sec < 1 { // under 1 MB/s on average is normal background work
			return Problem{}, false
		}
		return Problem{Time: ms, Kind: "disk", App: app,
			Title:  fmt.Sprintf("%s wrote %s to disk in %s", app, human(uint64(amt*(1<<20))), dur(time.Duration(sec)*time.Second)),
			Detail: "Heavy disk writing can make the computer feel slow while it happens."}, true
	}
	return Problem{}, false
}
