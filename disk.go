package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// Disk usage explorer: scans a folder tree once, then answers "what is big
// inside this folder?" for any folder in it, instantly.

const (
	keepFiles  = 40       // biggest files remembered per folder (enough for any top-40 list)
	pruneBelow = 32 << 20 // folders smaller than this forget their insides; reopened live
)

type fileEnt struct {
	name string
	size int64
	mod  int64
}

type dnode struct {
	name    string
	parent  *dnode
	size    int64
	files   int64
	dirs    []*dnode
	big     []fileEnt // biggest files directly inside, largest first
	rest    int64     // bytes of the other files directly inside
	restN   int64
	denied  bool // could not be read
	pruned  bool // small folder whose insides were dropped to save memory
	scanned bool
}

type diskScan struct {
	mu       sync.Mutex
	root     *dnode
	rootPath string
	label    string
	started  time.Time
	took     time.Duration
	running  bool
	gen      int64
	cancel   atomic.Bool
	files    atomic.Int64
	bytes    atomic.Int64
	denied   atomic.Int64
	sem      chan struct{}
}

func newDiskScan() *diskScan { return &diskScan{sem: make(chan struct{}, 2*runtime.NumCPU())} }

// skipDirs are virtual or duplicate trees that would double-count or hang.
var skipDirs = map[string]bool{
	"/proc": true, "/sys": true, "/dev": true, "/run": true,
	"/System/Volumes": true, "/Volumes": true, "/private/var/vm": true,
	"/Users/Shared/.dropbox": true,
}

func (d *diskScan) start(path string) (string, error) {
	home, _ := os.UserHomeDir()
	label := path
	if path == "" || filepath.Clean(path) == filepath.Clean(home) {
		path, label = home, "Home"
	} else if filepath.Clean(path) == filepath.Clean(filepath.VolumeName(path)+string(filepath.Separator)) {
		label = "Whole disk"
	}
	// On macOS, "/" is a read-only system volume; everything you add lives on
	// the Data volume, which is what the "Whole disk" button should show.
	if runtime.GOOS == "darwin" && filepath.Clean(path) == "/" {
		path, label = "/System/Volumes/Data", "Whole disk"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", errors.New("that folder doesn't exist")
	}
	d.cancel.Store(true)
	d.mu.Lock()
	d.gen++
	gen := d.gen
	root := &dnode{name: abs}
	d.root, d.rootPath, d.label = root, abs, label
	d.started, d.took, d.running = time.Now(), 0, true
	d.files.Store(0)
	d.bytes.Store(0)
	d.denied.Store(0)
	d.mu.Unlock()
	d.cancel.Store(false)

	go func() {
		d.walk(root, abs, devOf(st), true)
		d.mu.Lock()
		if d.gen == gen {
			d.running, d.took = false, time.Since(d.started)
		}
		d.mu.Unlock()
	}()
	return abs, nil
}

func (d *diskScan) walk(n *dnode, path string, dev uint64, top bool) {
	n.scanned = true
	entries, err := os.ReadDir(path)
	if err != nil {
		n.denied = true
		d.denied.Add(1)
		return
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		if d.cancel.Load() {
			break
		}
		full := filepath.Join(path, e.Name())
		info, err := e.Info() // Lstat: symlinks are not followed
		if err != nil {
			continue
		}
		if info.IsDir() {
			if skipDirs[full] || (dev != 0 && devOf(info) != dev) {
				continue // other disks and virtual file systems
			}
			c := &dnode{name: e.Name(), parent: n}
			n.dirs = append(n.dirs, c)
			select {
			case d.sem <- struct{}{}:
				wg.Add(1)
				go func() {
					defer wg.Done()
					d.walk(c, full, dev, false)
					<-d.sem
				}()
			default:
				d.walk(c, full, dev, false)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		sz := allocSize(info)
		n.addFile(fileEnt{e.Name(), sz, info.ModTime().UnixMilli()})
		d.files.Add(1)
		d.bytes.Add(sz)
	}
	wg.Wait()
	for _, c := range n.dirs {
		n.size += c.size
		n.files += c.files
	}
	sort.Slice(n.dirs, func(i, j int) bool { return n.dirs[i].size > n.dirs[j].size })
	if !top && n.size < pruneBelow && len(n.dirs)+len(n.big) > 0 {
		n.dirs, n.big, n.pruned = nil, nil, true
	}
}

func (n *dnode) addFile(f fileEnt) {
	n.size += f.size
	n.files++
	if len(n.big) < keepFiles || f.size > n.big[len(n.big)-1].size {
		i := sort.Search(len(n.big), func(i int) bool { return n.big[i].size < f.size })
		n.big = append(n.big, fileEnt{})
		copy(n.big[i+1:], n.big[i:])
		n.big[i] = f
		if len(n.big) > keepFiles {
			drop := n.big[keepFiles]
			n.rest += drop.size
			n.restN++
			n.big = n.big[:keepFiles]
		}
		return
	}
	n.rest += f.size
	n.restN++
}

// ---- answering questions about the finished scan

type DiskItem struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Dir    bool   `json:"dir"`
	Size   int64  `json:"size"`
	Files  int64  `json:"files,omitempty"`
	Mod    int64  `json:"mod,omitempty"`
	Denied bool   `json:"denied,omitempty"`
	Other  bool   `json:"other,omitempty"` // "N smaller files" summary row
}

type DiskView struct {
	State    string     `json:"state"` // none | scanning | done
	Root     string     `json:"root"`
	Label    string     `json:"label"`
	Path     string     `json:"path"`
	Crumbs   []DiskItem `json:"crumbs"`
	Size     int64      `json:"size"`
	Files    int64      `json:"files"`
	Items    []DiskItem `json:"items"`
	Largest  []DiskItem `json:"largest"`
	Scanned  int64      `json:"scanned"`
	Bytes    int64      `json:"bytes"`
	Denied   int64      `json:"denied"`
	Elapsed  float64    `json:"elapsed"`
	VolTotal uint64     `json:"volTotal"`
	VolUsed  uint64     `json:"volUsed"`
	Sep      string     `json:"sep"`
}

func (d *diskScan) view(path string) (DiskView, error) {
	d.mu.Lock()
	root, rootPath, label, running := d.root, d.rootPath, d.label, d.running
	elapsed := d.took
	if running {
		elapsed = time.Since(d.started)
	}
	d.mu.Unlock()

	v := DiskView{State: "none", Sep: string(filepath.Separator)}
	if root == nil {
		return v, nil
	}
	v.Root, v.Label, v.Elapsed = rootPath, label, elapsed.Seconds()
	v.Scanned, v.Bytes, v.Denied = d.files.Load(), d.bytes.Load(), d.denied.Load()
	if u, err := disk.Usage(rootPath); err == nil {
		v.VolTotal, v.VolUsed = u.Total, u.Total-u.Free
	}
	if running {
		v.State = "scanning"
		return v, nil
	}
	v.State = "done"

	n, abs, err := d.find(root, rootPath, path)
	if err != nil {
		return v, err
	}
	if n.pruned { // small folder: read it again now, it's quick
		d.mu.Lock()
		if n.pruned {
			fresh := &dnode{name: n.name, parent: n.parent}
			f, b, dn := d.files.Load(), d.bytes.Load(), d.denied.Load()
			d.walk(fresh, abs, 0, true)
			d.files.Store(f)
			d.bytes.Store(b)
			d.denied.Store(dn)
			n.dirs, n.big, n.rest, n.restN, n.pruned = fresh.dirs, fresh.big, fresh.rest, fresh.restN, false
			for _, c := range n.dirs {
				c.parent = n
			}
		}
		d.mu.Unlock()
	}
	v.Path, v.Size, v.Files = abs, n.size, n.files
	for p, at := n, abs; p != nil; p, at = p.parent, filepath.Dir(at) {
		name := p.name
		if p == root {
			name = label
		}
		v.Crumbs = append([]DiskItem{{Name: name, Path: at, Dir: true, Size: p.size}}, v.Crumbs...)
	}
	for _, c := range n.dirs {
		v.Items = append(v.Items, DiskItem{Name: c.name, Path: filepath.Join(abs, c.name), Dir: true,
			Size: c.size, Files: c.files, Denied: c.denied})
	}
	for _, f := range n.big {
		v.Items = append(v.Items, DiskItem{Name: f.name, Path: filepath.Join(abs, f.name), Size: f.size, Mod: f.mod})
	}
	if n.restN > 0 {
		v.Items = append(v.Items, DiskItem{Name: "", Path: abs, Size: n.rest, Files: n.restN, Other: true})
	}
	sort.SliceStable(v.Items, func(i, j int) bool { return v.Items[i].Size > v.Items[j].Size })
	if len(v.Items) > 300 {
		v.Items = v.Items[:300]
	}
	v.Largest = largestFiles(n, abs, 40)
	return v, nil
}

func (d *diskScan) find(root *dnode, rootPath, path string) (*dnode, string, error) {
	if path == "" {
		return root, rootPath, nil
	}
	rel, err := filepath.Rel(rootPath, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("that folder is outside the scanned area")
	}
	n := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			var next *dnode
			for _, c := range n.dirs {
				if c.name == part {
					next = c
					break
				}
			}
			if next == nil {
				return nil, "", errors.New("that folder was not found; try scanning again")
			}
			n = next
		}
	}
	return n, filepath.Join(rootPath, rel), nil
}

// largestFiles walks a subtree's remembered files. Each folder keeps its own
// top keepFiles, so the overall top-k (k <= keepFiles) is always exact.
func largestFiles(n *dnode, abs string, k int) []DiskItem {
	var out []DiskItem
	var visit func(n *dnode, at string)
	visit = func(n *dnode, at string) {
		for _, f := range n.big {
			if len(out) >= k && f.size <= out[len(out)-1].Size {
				break
			}
			out = append(out, DiskItem{Name: f.name, Path: filepath.Join(at, f.name), Size: f.size, Mod: f.mod})
			sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
			if len(out) > k {
				out = out[:k]
			}
		}
		for _, c := range n.dirs {
			if len(out) >= k && c.size <= out[len(out)-1].Size {
				continue // nothing inside can beat the current list
			}
			visit(c, filepath.Join(at, c.name))
		}
	}
	visit(n, abs)
	return out
}

// reveal shows a file or folder in Finder / Explorer / the file manager.
func (d *diskScan) reveal(path string) error {
	d.mu.Lock()
	rootPath := d.rootPath
	d.mu.Unlock()
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(rootPath, clean)
	if rootPath == "" || err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("that item is outside the scanned area")
	}
	if _, err := os.Lstat(clean); err != nil {
		return errors.New("that item no longer exists")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command(sysTool("open"), "-R", clean).Start()
	case "windows":
		// Explorer splits its /select option on commas, so a crafted name could
		// confuse it; for those, just open the folder that holds the item.
		if strings.ContainsAny(clean, ",\"") {
			return exec.Command(sysTool("explorer"), filepath.Dir(clean)).Start()
		}
		return exec.Command(sysTool("explorer"), "/select,", clean).Start()
	default:
		st, _ := os.Stat(clean)
		if st == nil || !st.IsDir() {
			clean = filepath.Dir(clean)
		}
		return exec.Command(sysTool("xdg-open"), clean).Start()
	}
}

type Place struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

// places lists sensible starting points: the home folder, the whole system
// disk, and any other drives that are plugged in.
func places() []Place {
	var out []Place
	add := func(label, path string) {
		p := Place{Label: label, Path: path}
		probe := path
		if runtime.GOOS == "darwin" && path == "/" {
			probe = "/System/Volumes/Data"
		}
		if u, err := disk.Usage(probe); err == nil {
			p.Total, p.Free = u.Total, u.Free
		}
		out = append(out, p)
	}
	home, _ := os.UserHomeDir()
	add("Home", home)
	sysRoot := "/"
	if runtime.GOOS == "windows" {
		sysRoot = os.Getenv("SystemDrive") + `\`
	}
	add("Whole disk", sysRoot)
	parts, _ := disk.Partitions(false)
	seen := map[string]bool{strings.ToUpper(sysRoot): true}
	for _, p := range parts {
		m := p.Mountpoint
		ok := false
		switch runtime.GOOS {
		case "windows":
			ok = len(m) <= 3
		case "darwin":
			ok = strings.HasPrefix(m, "/Volumes/")
		default:
			ok = strings.HasPrefix(m, "/mnt/") || strings.HasPrefix(m, "/media/") || strings.HasPrefix(m, "/run/media/") || m == "/home"
		}
		if ok && !seen[strings.ToUpper(m)] {
			seen[strings.ToUpper(m)] = true
			add(m, m)
		}
	}
	return out
}
