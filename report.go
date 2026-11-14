package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

type point5 struct {
	T      int64   `json:"t"`
	CPU    float64 `json:"cpu"`
	CPUMax float64 `json:"cpuMax"`
	Mem    float64 `json:"mem"`
	MemMax float64 `json:"memMax"`
	Swap   uint64  `json:"swap"`
}

type appTotal struct {
	Name    string  `json:"name"`
	CPUMin  float64 `json:"cpuMin"`    // minutes of the whole processor
	Share   float64 `json:"share"`     // % of all processor use in the period
	PeakMem uint64  `json:"peakMem"`   // highest memory seen
	PeakAt  int64   `json:"peakAt"`    // when
	Seen    float64 `json:"seenHours"` // hours it was among the top apps
}

type slowSpan struct {
	From   int64   `json:"from"`
	To     int64   `json:"to"`
	Kind   string  `json:"kind"` // memory | cpu
	Peak   float64 `json:"peak"`
	App    string  `json:"app,omitempty"`
	AppUse string  `json:"appUse,omitempty"`
}

type Report struct {
	From     int64      `json:"from"`
	To       int64      `json:"to"`
	Days     int        `json:"days"`
	Host     string     `json:"host"`
	OS       string     `json:"os"`
	Boot     int64      `json:"boot"`
	Hours    float64    `json:"hours"` // hours of history recorded in the period
	Series   []point5   `json:"series"`
	Gaps     [][2]int64 `json:"gaps"` // off, asleep, or TaskLens not running
	Apps     []appTotal `json:"apps"`
	Slow     []slowSpan `json:"slow"`
	Restarts []Restart  `json:"restarts"`
	Problems []Problem  `json:"problems"`
	Alerts   []Alert    `json:"alerts"`
	Summary  []string   `json:"summary"`
}

type reporter struct {
	rec   *recorder
	ins   *insights
	mon   *monitor
	col   *collector
	mu    sync.Mutex
	cache map[int]cached
}

type cached struct {
	at time.Time
	r  Report
}

func (rp *reporter) build(days int) Report {
	if days < 1 || days > keepDays {
		days = keepDays
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if c, ok := rp.cache[days]; ok && time.Since(c.at) < time.Minute {
		return c.r
	}
	now := time.Now()
	since := now.AddDate(0, 0, -days)
	slots := rp.rec.slots(since)
	snap := rp.col.last()
	r := Report{From: since.UnixMilli(), To: now.UnixMilli(), Days: days, Host: snap.System.Host, OS: snap.System.OS,
		Hours: float64(len(slots)) * bucketSize.Hours()}
	if bt, err := host.BootTime(); err == nil {
		r.Boot = int64(bt) * 1000
	}
	r.Series, r.Gaps = series(slots, since, now)
	r.Apps = appTotals(slots)
	r.Slow = slowSpans(slots)
	r.Restarts = rp.ins.restarts(since, slots)
	if r.Restarts == nil {
		r.Restarts = []Restart{}
	}
	sig, _ := rp.ins.get(since)
	r.Problems = groupProblems(append([]Problem(nil), sig.problems...), since)
	if len(r.Problems) > 60 {
		r.Problems = r.Problems[:60]
	}
	r.Alerts = []Alert{}
	for _, a := range rp.mon.recent() {
		if a.Time >= since.UnixMilli() {
			r.Alerts = append(r.Alerts, a)
		}
	}
	if r.Slow == nil {
		r.Slow = []slowSpan{}
	}
	if r.Problems == nil {
		r.Problems = []Problem{}
	}
	r.Summary = summary(r)
	if rp.cache == nil {
		rp.cache = map[int]cached{}
	}
	rp.cache[days] = cached{now, r}
	return r
}

// series thins the 5-minute history to about 300 points for the chart and
// marks the gaps when nothing was recorded.
func series(slots []slot, since, now time.Time) ([]point5, [][2]int64) {
	gaps := [][2]int64{}
	prev := since.UnixMilli()
	for _, s := range slots {
		if s.T-prev > int64(15*time.Minute/time.Millisecond) {
			gaps = append(gaps, [2]int64{prev, s.T})
		}
		prev = s.T + bucketSize.Milliseconds()
	}
	if now.UnixMilli()-prev > int64(15*time.Minute/time.Millisecond) {
		gaps = append(gaps, [2]int64{prev, now.UnixMilli()})
	}
	step := (len(slots) + 299) / 300
	if step < 1 {
		step = 1
	}
	pts := []point5{}
	for i := 0; i < len(slots); {
		p := point5{T: slots[i].T}
		j := i
		for ; j < len(slots) && j < i+step; j++ {
			if j > i && slots[j].T-slots[j-1].T > int64(15*time.Minute/time.Millisecond) {
				break // don't average across a gap
			}
			s := slots[j]
			p.CPU += s.CPU
			p.Mem += s.Mem
			p.CPUMax, p.MemMax, p.Swap = max(p.CPUMax, s.CPUMax), max(p.MemMax, s.MemMax), max(p.Swap, s.Swap)
		}
		n := float64(j - i)
		p.CPU, p.Mem = round1(p.CPU/n), round1(p.Mem/n)
		pts = append(pts, p)
		i = j
	}
	return pts, gaps
}

func appTotals(slots []slot) []appTotal {
	m := map[string]*appTotal{}
	var total float64
	for _, s := range slots {
		total += s.CPU * bucketSize.Minutes() / 100
		for _, a := range s.Top {
			t := m[a.Name]
			if t == nil {
				t = &appTotal{Name: a.Name}
				m[a.Name] = t
			}
			t.CPUMin += a.CPU * bucketSize.Minutes() / 100
			t.Seen += bucketSize.Hours()
			if a.Mem > t.PeakMem {
				t.PeakMem, t.PeakAt = a.Mem, s.T
			}
		}
	}
	out := make([]appTotal, 0, len(m))
	for _, t := range m {
		if total > 0 {
			t.Share = round1(min(100, t.CPUMin/total*100))
		}
		t.CPUMin, t.Seen = round1(t.CPUMin), round1(t.Seen)
		out = append(out, *t)
	}
	// Rank by a mix of processor time and memory so both kinds of heavy app show.
	score := func(t appTotal) float64 { return t.Share + float64(t.PeakMem)/(1<<30) }
	sort.Slice(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// slowSpans finds the times the computer was most likely slow or stuck:
// memory over 90% full, or the processor over 90% busy, for 10 minutes or more.
func slowSpans(slots []slot) []slowSpan {
	var out []slowSpan
	for _, kind := range []string{"memory", "cpu"} {
		var cur *slowSpan
		var appBest uint64
		var appCPU float64
		closeSpan := func() {
			if cur != nil && (cur.To-cur.From >= 10*60_000 || cur.Peak >= 97) {
				out = append(out, *cur)
			}
			cur, appBest, appCPU = nil, 0, 0
		}
		for _, s := range slots {
			v, hot := s.MemMax, s.MemMax >= 90
			if kind == "cpu" {
				v, hot = s.CPU, s.CPU >= 90
			}
			if !hot || (cur != nil && s.T-cur.To > 10*60_000) {
				closeSpan()
			}
			if !hot {
				continue
			}
			if cur == nil {
				cur = &slowSpan{From: s.T, Kind: kind}
			}
			cur.To = s.T + bucketSize.Milliseconds()
			cur.Peak = max(cur.Peak, v)
			for _, a := range s.Top {
				if kind == "memory" && a.Mem > appBest {
					appBest, cur.App, cur.AppUse = a.Mem, a.Name, human(a.Mem)
				}
				if kind == "cpu" && a.CPU > appCPU {
					appCPU, cur.App, cur.AppUse = a.CPU, a.Name, fmt.Sprintf("%.0f%% of the processor", a.CPU)
				}
			}
		}
		closeSpan()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From > out[j].From })
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

func when(ms int64) string { return time.UnixMilli(ms).Format("Mon 2 Jan 15:04") }

func summary(r Report) []string {
	var out []string
	period := fmt.Sprintf("the last %d days", r.Days)
	if r.Days == 1 {
		period = "the last 24 hours"
	}
	if n := len(r.Restarts); n == 0 {
		out = append(out, "No restarts in "+period+".")
	} else {
		kinds := map[string]int{}
		for _, x := range r.Restarts {
			kinds[x.Kind]++
		}
		var parts []string
		for _, k := range []struct{ key, label string }{{"crash", "crashed"}, {"unexpected", "turned off unexpectedly"},
			{"update", "for updates"}, {"normal", "normal"}, {"unknown", "reason unknown"}} {
			if kinds[k.key] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", kinds[k.key], k.label))
			}
		}
		times := "times"
		if n == 1 {
			times = "time"
		}
		out = append(out, fmt.Sprintf("Restarted %d %s in %s (%s). Latest: %s.", n, times, period, strings.Join(parts, ", "), when(r.Restarts[0].Boot)))
	}
	var mem, crash, hang int
	var memApp string
	for _, p := range r.Problems {
		c := max(p.Count, 1)
		switch p.Kind {
		case "memory":
			mem += c
			if memApp == "" {
				memApp = p.App
			}
		case "crash":
			crash += c
		case "hang":
			hang += c
		}
	}
	if mem > 0 {
		s := fmt.Sprintf("Memory ran out %d times, so the system closed apps to keep going.", mem)
		if memApp != "" {
			s += " The biggest app the latest time: " + memApp + "."
		}
		out = append(out, s)
	}
	if crash+hang > 0 {
		out = append(out, fmt.Sprintf("%d app crashes and %d times an app stopped responding.", crash, hang))
	}
	if len(r.Slow) > 0 {
		var longest slowSpan
		for _, s := range r.Slow {
			if s.To-s.From > longest.To-longest.From {
				longest = s
			}
		}
		what := "memory was over 90% full"
		if longest.Kind == "cpu" {
			what = "the processor was over 90% busy"
		}
		s := fmt.Sprintf("Slowest stretch: %s for %s, from %s", what, dur(time.Duration(longest.To-longest.From)*time.Millisecond), when(longest.From))
		if longest.App != "" {
			s += fmt.Sprintf(" (%s was using %s)", longest.App, longest.AppUse)
		}
		out = append(out, s+".")
	} else if r.Hours >= 1 {
		out = append(out, "No long slow periods: memory and processor stayed under 90%.")
	}
	if len(r.Apps) > 0 {
		busiest := r.Apps[0]
		for _, a := range r.Apps {
			if a.Share > busiest.Share {
				busiest = a
			}
		}
		biggest := r.Apps[0]
		for _, a := range r.Apps {
			if a.PeakMem > biggest.PeakMem {
				biggest = a
			}
		}
		if busiest.Share >= 1 {
			out = append(out, fmt.Sprintf("Busiest app: %s, %.0f%% of all processor use.", busiest.Name, busiest.Share))
		}
		out = append(out, fmt.Sprintf("Most memory: %s, up to %s (%s).", biggest.Name, human(biggest.PeakMem), when(biggest.PeakAt)))
	}
	if r.Hours < 1 {
		out = append(out, "TaskLens has only just started recording, so the chart and app list fill in over the next hours and days. Restarts and problems come from the system's own records and already cover the whole period.")
	} else if r.Hours < float64(r.Days)*24*0.5 {
		out = append(out, fmt.Sprintf("TaskLens recorded %.0f hours of this period; turn on Start at login so the history has no gaps.", r.Hours))
	}
	return out
}

// text is the plain version for email.
func (r Report) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "TaskLens report for %s (%s)\n%s to %s\n\n", r.Host, r.OS, when(r.From), when(r.To))
	for _, s := range r.Summary {
		b.WriteString("• " + s + "\n")
	}
	if len(r.Restarts) > 0 {
		b.WriteString("\nRESTARTS\n")
		for _, x := range r.Restarts {
			fmt.Fprintf(&b, "• %s: %s\n", when(x.Boot), x.Reason)
			if x.Before != "" {
				b.WriteString("  " + x.Before + "\n")
			}
			if x.Note != "" {
				b.WriteString("  " + x.Note + "\n")
			}
		}
	}
	if len(r.Slow) > 0 {
		b.WriteString("\nSLOW TIMES\n")
		for i, s := range r.Slow {
			if i == 10 {
				break
			}
			what := "memory full"
			if s.Kind == "cpu" {
				what = "processor busy"
			}
			fmt.Fprintf(&b, "• %s to %s: %s (up to %.0f%%)", when(s.From), time.UnixMilli(s.To).Format("15:04"), what, s.Peak)
			if s.App != "" {
				fmt.Fprintf(&b, ", %s using %s", s.App, s.AppUse)
			}
			b.WriteString("\n")
		}
	}
	if len(r.Apps) > 0 {
		b.WriteString("\nAPPS THAT USED THE MOST\n")
		for i, a := range r.Apps {
			if i == 8 {
				break
			}
			fmt.Fprintf(&b, "• %s: %.0f%% of processor use, up to %s memory\n", a.Name, a.Share, human(a.PeakMem))
		}
	}
	if len(r.Problems) > 0 {
		b.WriteString("\nPROBLEMS THE SYSTEM RECORDED\n")
		for i, p := range r.Problems {
			if i == 15 {
				break
			}
			fmt.Fprintf(&b, "• %s: %s", when(p.Time), p.Title)
			if p.Count > 1 {
				fmt.Fprintf(&b, " (%d times, until %s)", p.Count, time.UnixMilli(p.Last).Format("15:04"))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\nOpen TaskLens → Report on that computer for the charts and details.\n" +
		"Sent by TaskLens. To change or stop these emails, open TaskLens → Alerts.\n")
	return b.String()
}
