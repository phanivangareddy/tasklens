package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

// Restart explains one time the computer started up again.
type Restart struct {
	Boot   int64  `json:"boot"`           // when it came back, unix ms
	Down   int64  `json:"down,omitempty"` // when it went down, if known
	Kind   string `json:"kind"`           // normal | update | crash | unexpected | unknown
	Reason string `json:"reason"`
	Before string `json:"before,omitempty"` // what was happening just before
	Note   string `json:"note,omitempty"`
}

// Problem is something the operating system recorded: an app crash, an app
// that stopped responding, apps closed because memory ran out, and so on.
type Problem struct {
	Time   int64  `json:"time"`
	Last   int64  `json:"last,omitempty"` // when several in a row are grouped
	Count  int    `json:"count,omitempty"`
	Kind   string `json:"kind"` // crash | hang | memory | cpu | disk | system
	App    string `json:"app,omitempty"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// signals is what each operating system can tell us; see events_<os>.go.
type signals struct {
	boots          []time.Time
	shutdowns      []time.Time // clean shut-downs / restarts
	knowsShutdowns bool        // false when the system keeps no such record (or we can't read it)
	crashes        []sysEvent  // kernel panic, blue screen…
	unexpected     []time.Time // "the previous shutdown was unexpected"
	initiated      []sysEvent  // who asked for a restart (Windows)
	slowShutdowns  []sysEvent
	problems       []Problem
}

type sysEvent struct {
	t      time.Time
	text   string
	update bool // restart was for a system update
}

type osUpdate struct {
	T    int64  `json:"t"`
	From string `json:"from"`
	To   string `json:"to"`
}

type eventState struct {
	LastBoot int64      `json:"lastBoot"`
	OS       string     `json:"os"`
	Updates  []osUpdate `json:"updates,omitempty"`
	Weekly   int64      `json:"weekly,omitempty"` // when the weekly email was last sent
	// Reasons already found, kept because macOS and Windows delete their own
	// crash records after a while.
	Known []Restart `json:"known,omitempty"`
}

// insights caches what the operating system said for ten minutes: reading
// system logs is the only costly part of the report, so it's done rarely.
type insights struct {
	rec    *recorder
	osName string
	mu     sync.Mutex
	at     time.Time
	sig    signals
	state  eventState
}

func newInsights(rec *recorder, osName string) *insights {
	in := &insights{rec: rec, osName: osName}
	in.noteBoot()
	return in
}

func eventsPath() string { return filepath.Join(configDir(), "events.json") }

// noteBoot remembers this boot and spots operating system updates between boots.
func (in *insights) noteBoot() {
	if raw, err := os.ReadFile(eventsPath()); err == nil {
		json.Unmarshal(raw, &in.state)
	}
	bt, err := host.BootTime()
	if err != nil {
		return
	}
	boot := int64(bt) * 1000
	st := &in.state
	if st.LastBoot != 0 && abs64(st.LastBoot-boot) > 120_000 && st.OS != "" && st.OS != in.osName {
		st.Updates = append(st.Updates, osUpdate{T: boot, From: st.OS, To: in.osName})
	}
	cut := time.Now().AddDate(0, 0, -keepDays).UnixMilli()
	kept := st.Updates[:0]
	for _, u := range st.Updates {
		if u.T >= cut {
			kept = append(kept, u)
		}
	}
	st.Updates, st.LastBoot, st.OS = kept, boot, in.osName
	in.saveState()
}

func (in *insights) saveState() {
	raw, _ := json.Marshal(in.state)
	os.MkdirAll(configDir(), 0o700)
	os.WriteFile(eventsPath(), raw, 0o600)
}

// weeklyDue says whether a week has passed since the last weekly email
// (sent in the morning, not in the middle of the night).
func (in *insights) weeklyDue(now time.Time) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return now.Sub(time.UnixMilli(in.state.Weekly)) >= 7*24*time.Hour-time.Hour && now.Hour() >= 9 && now.Hour() < 21
}

func (in *insights) weeklySent(now time.Time) {
	in.mu.Lock()
	in.state.Weekly = now.UnixMilli()
	in.saveState()
	in.mu.Unlock()
}

func (in *insights) get(since time.Time) (signals, []osUpdate) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if time.Since(in.at) > 10*time.Minute {
		in.sig = readSignals(time.Now().AddDate(0, 0, -keepDays))
		in.at = time.Now()
	}
	return in.sig, in.state.Updates
}

// restarts pairs every boot with whatever explains it.
func (in *insights) restarts(since time.Time, slots []slot) []Restart {
	sig, updates := in.get(since)
	boots := append([]time.Time(nil), sig.boots...)
	if bt, err := host.BootTime(); err == nil {
		boots = append(boots, time.Unix(int64(bt), 0))
	}
	sort.Slice(boots, func(i, j int) bool { return boots[i].Before(boots[j]) })
	var uniq []time.Time
	for _, b := range boots { // the same boot can come from two sources, a minute apart
		if n := len(uniq); n > 0 && b.Sub(uniq[n-1]) < 3*time.Minute {
			if b.After(uniq[n-1]) && b.Sub(uniq[n-1]) < time.Minute {
				uniq[n-1] = b // keep the precise one
			}
			continue
		}
		uniq = append(uniq, b)
	}

	var out []Restart
	for i, b := range uniq {
		if b.Before(since) {
			continue
		}
		prev := b.AddDate(0, 0, -7)
		if i > 0 {
			prev = uniq[i-1]
		}
		near := func(t time.Time) bool { return !t.Before(b.Add(-30*time.Minute)) && !t.After(b.Add(30*time.Minute)) }
		between := func(t time.Time) bool { return t.After(prev.Add(time.Minute)) && !t.After(b.Add(time.Minute)) }

		r := Restart{Boot: b.UnixMilli(), Kind: "unknown",
			Reason: "TaskLens couldn't find a record of why. It may have been turned off normally."}
		var down time.Time
		for _, s := range sig.shutdowns {
			if between(s) {
				down = s
			}
		}
		var crash *sysEvent
		for k := range sig.crashes {
			if near(sig.crashes[k].t) || between(sig.crashes[k].t) {
				crash = &sig.crashes[k]
			}
		}
		var by *sysEvent
		for k := range sig.initiated {
			if between(sig.initiated[k].t) {
				by = &sig.initiated[k]
			}
		}
		unexpected := false
		for _, u := range sig.unexpected {
			if near(u) {
				unexpected = true
			}
		}
		var upd *osUpdate
		for k := range updates {
			if abs64(updates[k].T-b.UnixMilli()) < 5*60_000 {
				upd = &updates[k]
			}
		}
		lastSeen := lastSlotBefore(slots, prev, b)

		switch {
		case crash != nil:
			r.Kind, r.Reason = "crash", crash.text
		case upd != nil:
			r.Kind, r.Reason = "update", fmt.Sprintf("The system was updated from %s to %s.", upd.From, upd.To)
		case by != nil && by.update:
			r.Kind, r.Reason = "update", by.text
		case by != nil:
			r.Kind, r.Reason = "normal", by.text
		case !down.IsZero():
			r.Kind, r.Reason = "normal", "Shut down or restarted normally (from the menu, an app, or an update)."
		case unexpected || sig.knowsShutdowns:
			r.Kind = "unexpected"
			r.Reason = "It turned off without shutting down properly: the power was cut, the power button was held down, or the computer froze."
		}
		if down.IsZero() && by != nil {
			down = by.t
		}
		if down.IsZero() && !lastSeen.IsZero() {
			down = lastSeen
		}
		if !down.IsZero() {
			r.Down = down.UnixMilli()
			before, memFull := summarizeBefore(slots, down)
			r.Before = before
			if memFull && r.Kind == "unexpected" {
				r.Reason += " Memory was almost full just before, so it most likely froze."
			}
		}
		for _, s := range sig.slowShutdowns {
			if between(s.t) {
				r.Note = s.text
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Boot > out[j].Boot })
	in.remember(out)
	return out
}

// remember keeps every restart whose reason was found, and fills in reasons
// for restarts whose records the system has since deleted.
func (in *insights) remember(rs []Restart) {
	in.mu.Lock()
	defer in.mu.Unlock()
	sure := func(k string) bool { return k == "crash" || k == "update" || k == "normal" }
	changed := false
	for i := range rs {
		r := &rs[i]
		for _, k := range in.state.Known {
			if abs64(k.Boot-r.Boot) < 3*60_000 && !sure(r.Kind) {
				r.Kind, r.Reason = k.Kind, k.Reason
				if r.Note == "" {
					r.Note = k.Note
				}
			}
		}
		if !sure(r.Kind) {
			continue
		}
		found := false
		for j := range in.state.Known {
			if abs64(in.state.Known[j].Boot-r.Boot) < 3*60_000 {
				found = true
			}
		}
		if !found {
			in.state.Known = append(in.state.Known, Restart{Boot: r.Boot, Kind: r.Kind, Reason: r.Reason, Note: r.Note})
			changed = true
		}
	}
	cut := time.Now().AddDate(0, 0, -keepDays).UnixMilli()
	kept := in.state.Known[:0]
	for _, k := range in.state.Known {
		if k.Boot >= cut {
			kept = append(kept, k)
		} else {
			changed = true
		}
	}
	in.state.Known = kept
	if changed {
		in.saveState()
	}
}

func lastSlotBefore(slots []slot, after, before time.Time) time.Time {
	var t time.Time
	for _, s := range slots {
		st := time.UnixMilli(s.T)
		if st.After(after) && st.Before(before) {
			t = st.Add(bucketSize)
		}
	}
	if t.After(before) {
		t = before
	}
	return t
}

// summarizeBefore describes the 30 minutes before t, from our own history.
func summarizeBefore(slots []slot, t time.Time) (string, bool) {
	from := t.Add(-30 * time.Minute).UnixMilli()
	var memMax, cpuMax float64
	var swap uint64
	apps := map[string]uint64{}
	n := 0
	for _, s := range slots {
		if s.T < from || s.T > t.UnixMilli() {
			continue
		}
		n++
		memMax, cpuMax, swap = max(memMax, s.MemMax), max(cpuMax, s.CPUMax), max(swap, s.Swap)
		for _, a := range s.Top {
			apps[a.Name] = max(apps[a.Name], a.Mem)
		}
	}
	if n == 0 {
		return "", false
	}
	msg := fmt.Sprintf("In the 30 minutes before: memory up to %.0f%%, processor up to %.0f%%", memMax, cpuMax)
	if swap >= 1<<30 {
		msg += ", " + human(swap) + " moved to disk"
	}
	if top := topByMem(apps, 3); top != "" {
		msg += ". Biggest apps: " + top
	}
	return msg + ".", memMax >= 95
}

func topByMem(apps map[string]uint64, n int) string {
	names := make([]string, 0, len(apps))
	for k := range apps {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return apps[names[i]] > apps[names[j]] })
	var parts []string
	for i := 0; i < n && i < len(names); i++ {
		parts = append(parts, names[i]+" "+human(apps[names[i]]))
	}
	return strings.Join(parts, ", ")
}

// groupProblems merges repeats of the same thing within an hour, newest first.
func groupProblems(ps []Problem, since time.Time) []Problem {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Time < ps[j].Time })
	var out []Problem
	for _, p := range ps {
		if p.Time < since.UnixMilli() {
			continue
		}
		if n := len(out); n > 0 {
			o := &out[n-1]
			last := max(o.Last, o.Time)
			// Memory running out is one event however many apps were involved.
			if o.Kind == p.Kind && (o.App == p.App || p.Kind == "memory") && p.Time-last <= 3600_000 {
				o.Last, o.Count = p.Time, max(o.Count, 1)+1
				o.Title, o.Detail, o.App = p.Title, p.Detail, p.App // describe the latest
				continue
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return max(out[i].Last, out[i].Time) > max(out[j].Last, out[j].Time) })
	return out
}

// run executes a small system tool quietly, with a time limit.
func runTool(timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.Output(); close(done) }()
	select {
	case <-done:
		return out, err
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done
		return nil, fmt.Errorf("%s took too long", name)
	}
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
