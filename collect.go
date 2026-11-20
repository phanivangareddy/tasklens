package main

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Proc struct {
	PID     int32   `json:"pid"`
	PPID    int32   `json:"ppid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	Cmd     string  `json:"cmd"`
	CPU     float64 `json:"cpu"`     // % of the whole machine, like Windows Task Manager
	Mem     uint64  `json:"mem"`     // bytes; on macOS the same "Memory" figure Activity Monitor shows
	Started int64   `json:"started"` // unix ms
	CPUSec  float64 `json:"cpuSec"`  // total processor time used since it started
	Paused  bool    `json:"paused"`
	System  bool    `json:"system"` // owned by root / SYSTEM / another account
}

type System struct {
	OS       string    `json:"os"`
	Host     string    `json:"host"`
	Cores    int       `json:"cores"`
	CPU      float64   `json:"cpu"`
	MemTotal uint64    `json:"memTotal"`
	MemUsed  uint64    `json:"memUsed"`
	Kernel   uint64    `json:"kernel,omitempty"` // Windows: kernel and drivers, owned by no process
	SwapUsed uint64    `json:"swapUsed"`
	SwapTot  uint64    `json:"swapTotal"`
	Uptime   uint64    `json:"uptime"`
	Load     []float64 `json:"load,omitempty"`
	DiskFree uint64    `json:"diskFree"`
	DiskTot  uint64    `json:"diskTotal"`
}

type Snapshot struct {
	Time    int64  `json:"time"`
	Version string `json:"version"`
	Me      string `json:"me"`
	System  System `json:"system"`
	Procs   []Proc `json:"procs"`
}

// static holds details that never change for a process, so they are read once.
type static struct {
	name, user, cmd string
	ppid            int32
}

type prevCPU struct {
	sec float64
	at  time.Time
}

type collector struct {
	sampling sync.Mutex // one sample at a time; it owns statics
	mu       sync.Mutex
	snap     []byte
	lastSnap Snapshot
	lastAt   time.Time
	statics  map[int64]static // keyed by pid<<32 ^ create time, so reused PIDs don't collide
	prev     map[int32]prevCPU
	paused   map[int32]bool
	active   chan struct{}
	me       string
	hostName string
	osName   string
}

func newCollector() *collector {
	c := &collector{
		statics: map[int64]static{},
		prev:    map[int32]prevCPU{},
		paused:  map[int32]bool{},
		active:  make(chan struct{}, 1),
	}
	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		c.me, _ = p.Username()
	}
	if hi, err := host.Info(); err == nil {
		c.hostName = hi.Hostname
		platform := hi.Platform
		if platform == "darwin" {
			platform = "macOS"
		}
		c.osName = strings.TrimSpace(platform + " " + hi.PlatformVersion)
	}
	if c.osName == "" {
		c.osName = runtime.GOOS
	}
	cpu.Percent(0, false)
	c.sample()
	return c
}

// touch marks the dashboard as open; sampling pauses when nobody is watching.
func (c *collector) touch() {
	select {
	case c.active <- struct{}{}:
	default:
	}
}

func (c *collector) run() {
	last := time.Now()
	for {
		select {
		case <-c.active:
			last = time.Now()
		case <-time.After(2 * time.Second):
		}
		if time.Since(last) < 15*time.Second {
			c.sample()
		}
		time.Sleep(2 * time.Second)
	}
}

func (c *collector) snapshotJSON() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *collector) sample() {
	c.sampling.Lock()
	defer c.sampling.Unlock()
	now := time.Now()
	ncpu := runtime.NumCPU()
	procs, _ := process.Processes()
	out := make([]Proc, 0, len(procs))
	seen := make(map[int64]bool, len(procs))
	nextPrev := make(map[int32]prevCPU, len(procs))

	for _, p := range procs {
		created, err := p.CreateTime()
		if err != nil {
			continue
		}
		key := int64(p.Pid)<<32 ^ created
		seen[key] = true
		st, ok := c.statics[key]
		if !ok {
			st.name, _ = p.Name()
			st.user, _ = p.Username()
			st.cmd, _ = p.Cmdline()
			st.name, st.user, st.cmd = oneLine(st.name), oneLine(st.user), oneLine(st.cmd)
			st.ppid, _ = p.Ppid()
			if st.name == "" {
				continue
			}
			c.statics[key] = st
		}
		pr := Proc{PID: p.Pid, PPID: st.ppid, Name: st.name, User: st.user, Cmd: st.cmd, Started: created}
		pr.System = st.user != "" && st.user != c.me
		if t, err := p.Times(); err == nil {
			pr.CPUSec = t.User + t.System
			if old, ok := c.prev[p.Pid]; ok {
				if dt := now.Sub(old.at).Seconds(); dt > 0 {
					pr.CPU = max(0, (pr.CPUSec-old.sec)/dt/float64(ncpu)*100)
				}
			}
			nextPrev[p.Pid] = prevCPU{pr.CPUSec, now}
		}
		if fp := physFootprint(p.Pid); fp > 0 {
			pr.Mem = fp
		} else if mi, err := p.MemoryInfo(); err == nil {
			pr.Mem = mi.RSS
		}
		pr.Paused = c.isPaused(p)
		out = append(out, pr)
	}
	for k := range c.statics {
		if !seen[k] {
			delete(c.statics, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mem > out[j].Mem })

	snap := Snapshot{Time: now.UnixMilli(), Version: version, Me: c.me, Procs: out,
		System: System{OS: c.osName, Host: c.hostName, Cores: ncpu}}
	s := &snap.System
	if v, err := cpu.Percent(0, false); err == nil && len(v) > 0 {
		s.CPU = v[0]
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemTotal, s.MemUsed = vm.Total, vm.Total-vm.Available
	}
	s.Kernel = kernelMemory()
	if sw, err := mem.SwapMemory(); err == nil {
		s.SwapUsed, s.SwapTot = sw.Used, sw.Total
	}
	s.Uptime, _ = host.Uptime()
	if runtime.GOOS != "windows" {
		if l, err := load.Avg(); err == nil {
			s.Load = []float64{l.Load1, l.Load5, l.Load15}
		}
	}
	root := "/"
	if runtime.GOOS == "windows" {
		root = os.Getenv("SystemDrive") + `\`
	}
	if du, err := disk.Usage(root); err == nil {
		s.DiskFree, s.DiskTot = du.Free, du.Total
	}

	raw, _ := json.Marshal(snap)
	c.mu.Lock()
	c.snap, c.prev = raw, nextPrev
	c.lastSnap, c.lastAt = snap, now
	c.mu.Unlock()
}

// last is the newest sample; age says how old it is.
func (c *collector) last() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSnap
}

func (c *collector) age() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastAt)
}

// isPaused uses our own record of what we paused; on Linux it also reads the
// kernel's state, which is cheap there (gopsutil shells out to `ps` on macOS).
func (c *collector) isPaused(p *process.Process) bool {
	c.mu.Lock()
	mine := c.paused[p.Pid]
	c.mu.Unlock()
	if mine || runtime.GOOS != "linux" {
		return mine
	}
	st, err := p.Status()
	return err == nil && len(st) > 0 && st[0] == process.Stop
}

func (c *collector) act(pid int32, action string) error {
	if pid <= 1 || int(pid) == os.Getpid() {
		return errors.New("TaskLens won't change this process")
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return errors.New("that process has already ended")
	}
	switch action {
	case "end":
		err = p.Terminate()
	case "force":
		err = p.Kill()
	case "pause":
		err = p.Suspend()
	case "resume":
		err = p.Resume()
	default:
		return errors.New("unknown action")
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if errors.Is(err, os.ErrPermission) || strings.Contains(msg, "denied") || strings.Contains(msg, "not permitted") {
			return errors.New(adminHint())
		}
		return err
	}
	c.mu.Lock()
	switch action {
	case "pause":
		c.paused[pid] = true
	default:
		delete(c.paused, pid)
	}
	c.mu.Unlock()
	go func() { time.Sleep(300 * time.Millisecond); c.sample() }()
	return nil
}

func adminHint() string {
	switch runtime.GOOS {
	case "windows":
		return "Permission denied. This process belongs to Windows or another user; right-click TaskLens and choose \"Run as administrator\" to manage it."
	case "darwin":
		return "Permission denied. This process belongs to macOS or another user; run TaskLens from Terminal with sudo to manage it."
	default:
		return "Permission denied. This process belongs to the system or another user; start TaskLens with sudo to manage it."
	}
}
