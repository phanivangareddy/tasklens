package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The report keeps two weeks of history and never more.
const (
	keepDays   = 14
	bucketSize = 5 * time.Minute
	topPerSlot = 5
)

// slot is one 5-minute summary: 288 a day, about 100 KB a day on disk.
type slot struct {
	T      int64     `json:"t"` // start, unix ms
	N      int       `json:"n"` // samples in it
	CPU    float64   `json:"cpu"`
	CPUMax float64   `json:"cpuMax"`
	Mem    float64   `json:"mem"` // % of RAM in use
	MemMax float64   `json:"memMax"`
	Swap   uint64    `json:"swap"` // highest
	Disk   uint64    `json:"disk"` // free space, last seen
	Top    []appSlot `json:"top,omitempty"`
}

type appSlot struct {
	Name string  `json:"n"`
	CPU  float64 `json:"c"` // average % of the whole machine during the slot
	Mem  uint64  `json:"m"` // highest memory during the slot
}

type appAcc struct {
	cpu float64
	mem uint64
}

// recorder folds samples into the current slot and appends finished slots to
// one small file per day. Only the current slot is kept in memory.
type recorder struct {
	dir  string
	mu   sync.Mutex
	cur  slot
	apps map[string]*appAcc
	last time.Time // newest sample, so a restart can be dated
}

func newRecorder() *recorder {
	r := &recorder{dir: filepath.Join(configDir(), "history"), apps: map[string]*appAcc{}}
	r.prune(time.Now())
	return r
}

func (r *recorder) add(snap Snapshot) {
	s := snap.System
	if s.MemTotal == 0 {
		return
	}
	now := time.UnixMilli(snap.Time)
	start := now.Truncate(bucketSize).UnixMilli()
	// Group helpers by name (Chrome, Claude…) so an app shows as one line.
	byName := map[string]*appAcc{}
	for _, p := range snap.Procs {
		a := byName[p.Name]
		if a == nil {
			a = &appAcc{}
			byName[p.Name] = a
		}
		a.cpu += p.CPU
		a.mem += p.Mem
	}
	keep := topNames(byName, 8)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur.N > 0 && r.cur.T != start {
		r.flushLocked()
	}
	if r.cur.N == 0 {
		r.cur = slot{T: start}
	}
	c := &r.cur
	memPct := float64(s.MemUsed) / float64(s.MemTotal) * 100
	c.CPU = (c.CPU*float64(c.N) + s.CPU) / float64(c.N+1)
	c.Mem = (c.Mem*float64(c.N) + memPct) / float64(c.N+1)
	c.CPUMax, c.MemMax = max(c.CPUMax, s.CPU), max(c.MemMax, memPct)
	c.Swap = max(c.Swap, s.SwapUsed)
	if s.DiskTot > 0 {
		c.Disk = s.DiskFree
	}
	c.N++
	for _, name := range keep {
		a := r.apps[name]
		if a == nil {
			a = &appAcc{}
			r.apps[name] = a
		}
		a.cpu += byName[name].cpu
		a.mem = max(a.mem, byName[name].mem)
	}
	r.last = now
}

// topNames returns the n biggest by processor plus the n biggest by memory.
func topNames(m map[string]*appAcc, n int) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	pick := map[string]bool{}
	sort.Slice(names, func(i, j int) bool { return m[names[i]].cpu > m[names[j]].cpu })
	for i := 0; i < n && i < len(names); i++ {
		pick[names[i]] = true
	}
	sort.Slice(names, func(i, j int) bool { return m[names[i]].mem > m[names[j]].mem })
	for i := 0; i < n && i < len(names); i++ {
		pick[names[i]] = true
	}
	out := make([]string, 0, len(pick))
	for k := range pick {
		out = append(out, k)
	}
	return out
}

func (r *recorder) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur.N > 0 {
		r.flushLocked()
	}
}

func (r *recorder) flushLocked() {
	c := r.cur
	var all []appSlot
	for name, a := range r.apps {
		all = append(all, appSlot{Name: name, CPU: round1(a.cpu / float64(c.N)), Mem: a.mem})
	}
	// Keep the top few by processor and by memory.
	pick := map[string]appSlot{}
	sort.Slice(all, func(i, j int) bool { return all[i].CPU > all[j].CPU })
	for i := 0; i < 3 && i < len(all); i++ {
		pick[all[i].Name] = all[i]
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Mem > all[j].Mem })
	for i := 0; i < len(all) && len(pick) < topPerSlot; i++ {
		pick[all[i].Name] = all[i]
	}
	for _, a := range pick {
		c.Top = append(c.Top, a)
	}
	sort.Slice(c.Top, func(i, j int) bool { return c.Top[i].Mem > c.Top[j].Mem })
	c.CPU, c.Mem, c.CPUMax, c.MemMax = round1(c.CPU), round1(c.Mem), round1(c.CPUMax), round1(c.MemMax)
	r.cur, r.apps = slot{}, map[string]*appAcc{}

	line, _ := json.Marshal(c)
	os.MkdirAll(r.dir, 0o700)
	f, err := os.OpenFile(r.dayFile(time.UnixMilli(c.T)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
	f.Close()
	r.prune(time.UnixMilli(c.T))
}

func (r *recorder) dayFile(t time.Time) string {
	return filepath.Join(r.dir, t.Format("2006-01-02")+".jsonl")
}

// prune deletes day files older than two weeks.
func (r *recorder) prune(now time.Time) {
	cut := now.AddDate(0, 0, -keepDays).Format("2006-01-02")
	ents, _ := os.ReadDir(r.dir)
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		if name != e.Name() && name < cut {
			os.Remove(filepath.Join(r.dir, e.Name()))
		}
	}
}

// slots reads the saved summaries from since until now, oldest first,
// including the slot still being filled.
func (r *recorder) slots(since time.Time) []slot {
	var out []slot
	for d := since; !d.After(time.Now().Add(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		f, err := os.Open(r.dayFile(d))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			var s slot
			if json.Unmarshal(sc.Bytes(), &s) == nil && s.T >= since.UnixMilli() {
				out = append(out, s)
			}
		}
		f.Close()
	}
	r.mu.Lock()
	if r.cur.N > 0 {
		c := r.cur
		for name, a := range r.apps {
			c.Top = append(c.Top, appSlot{Name: name, CPU: round1(a.cpu / float64(c.N)), Mem: a.mem})
		}
		out = append(out, c)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
