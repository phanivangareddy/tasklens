// TaskLens: a cross-platform task manager. It runs a small web server on
// 127.0.0.1 and opens the dashboard in the default browser, so one codebase
// works on Windows, macOS and Linux without a GUI toolkit.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

var version = "1.3.1"

// The app quits on its own this long after the last dashboard request,
// i.e. once every TaskLens tab has been closed.
const idleExit = 3 * time.Minute

type instance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func instanceFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "TaskLens", "instance.json")
}

// reuseRunning opens a tab on an already-running TaskLens instead of starting a second one.
func reuseRunning(open bool) bool {
	raw, err := os.ReadFile(instanceFile())
	if err != nil {
		return false
	}
	var in instance
	if json.Unmarshal(raw, &in) != nil {
		return false
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/ping", in.Port), nil)
	req.Header.Set("X-Token", in.Token)
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	if open {
		openBrowser(dashboardURL(in))
	}
	return true
}

func dashboardURL(in instance) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", in.Port, in.Token)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command(sysTool("open"), url)
	case "windows":
		cmd = exec.Command(sysTool("rundll32"), "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command(sysTool("xdg-open"), url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open a browser (%v); open %s yourself", err, url)
	}
}

func main() {
	port := flag.Int("port", 0, "port to listen on (0 picks a free one)")
	noBrowser := flag.Bool("no-browser", false, "don't open the dashboard automatically")
	stay := flag.Bool("stay", false, "keep running after the dashboard is closed")
	showVersion := flag.Bool("version", false, "print the version and exit")
	background := flag.Bool("background", false, "start quietly in the menu bar / tray (used at login)")
	noTray := flag.Bool("no-tray", false, "don't show a menu bar / tray icon")
	flag.Parse()
	if *showVersion {
		fmt.Println("TaskLens", version)
		return
	}
	if *port == 0 && reuseRunning(!*background) {
		return
	}
	// Stay as light as a task manager: collect garbage sooner, and keep the
	// heap under a soft cap so memory use can't creep up over weeks.
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(32 << 20)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	buf := make([]byte, 16)
	rand.Read(buf)
	in := instance{Port: ln.Addr().(*net.TCPAddr).Port, Token: hex.EncodeToString(buf)}
	if raw, err := json.Marshal(in); err == nil {
		os.MkdirAll(filepath.Dir(instanceFile()), 0o700)
		os.WriteFile(instanceFile(), raw, 0o600)
	}

	col := newCollector()
	go col.run()

	settings := loadSettings()
	mon := newMonitor(col, settings)
	ml := &mailer{settings: settings}
	openDash := func() { openBrowser(dashboardURL(in)) }
	pop := &popper{open: openDash, act: col.act}
	mon.notify = pop.show
	mon.email = func(a Alert) string { return ml.alert(a, col.last()) }
	rec := newRecorder()
	mon.record = rec.add
	ins := newInsights(rec, col.osName)
	rp := &reporter{rec: rec, ins: ins, mon: mon, col: col}
	go mon.run(func() bool { s := settings.get(); return s.Popups || s.Email.Enabled || s.History })
	go func() { // the weekly report email
		for {
			time.Sleep(time.Hour)
			s := settings.get()
			if s.Email.Enabled && s.Email.Weekly && ins.weeklyDue(time.Now()) {
				if err := ml.report(rp.build(7)); err != nil {
					log.Printf("weekly report email failed: %v", err)
				} else {
					ins.weeklySent(time.Now())
				}
			}
		}
	}()

	var lastSeen atomic.Int64
	lastSeen.Store(time.Now().UnixNano())
	quit := make(chan struct{}, 1)

	host := fmt.Sprintf("127.0.0.1:%d", in.Port)
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(static)))
	api := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			// The token stops other web pages from driving the API; the Host
			// check blocks DNS-rebinding tricks that point a domain at 127.0.0.1.
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(in.Token)) != 1 || r.Host != host {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			lastSeen.Store(time.Now().UnixNano())
			col.touch()
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // no request needs more
			h(w, r)
		})
	}
	api("/api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	api("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(col.snapshotJSON())
	})
	api("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			PID    int32  `json:"pid"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := col.act(req.PID, req.Action); err != nil {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	ds := newDiskScan()
	reply := func(w http.ResponseWriter, v any, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			v = map[string]string{"error": err.Error()}
		}
		json.NewEncoder(w).Encode(v)
	}
	var pathReq struct {
		Path string `json:"path"`
	}
	readPath := func(r *http.Request) (string, error) {
		if r.Method != http.MethodPost {
			return "", errors.New("POST only")
		}
		req := pathReq
		err := json.NewDecoder(r.Body).Decode(&req)
		return req.Path, err
	}
	api("/api/disk/places", func(w http.ResponseWriter, r *http.Request) { reply(w, places(), nil) })
	api("/api/disk/scan", func(w http.ResponseWriter, r *http.Request) {
		p, err := readPath(r)
		if err == nil {
			p, err = ds.start(p)
		}
		reply(w, map[string]string{"root": p}, err)
	})
	api("/api/disk", func(w http.ResponseWriter, r *http.Request) {
		v, err := ds.view(r.URL.Query().Get("path"))
		reply(w, v, err)
	})
	api("/api/disk/reveal", func(w http.ResponseWriter, r *http.Request) {
		p, err := readPath(r)
		if err == nil {
			err = ds.reveal(p)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	})
	settingsView := func() map[string]any {
		s := settings.get()
		hasPw := s.Email.Username != "" && settings.password(s.Email.Username) != ""
		fallback := s.Email.FallbackPassword != ""
		s.Email.FallbackPassword = ""
		return map[string]any{"settings": s, "hasPassword": hasPw, "keychain": !fallback,
			"presets": presets, "rules": ruleNames, "providers": providers, "os": runtime.GOOS}
	}
	api("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			reply(w, settingsView(), nil)
			return
		}
		var next Settings
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			reply(w, nil, errors.New("bad request"))
			return
		}
		if next.StartAtLogin != settings.get().StartAtLogin {
			if err := setStartAtLogin(next.StartAtLogin); err != nil {
				reply(w, nil, err)
				return
			}
		}
		if err := settings.save(next); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, settingsView(), nil)
	})
	api("/api/settings/password", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&req) != nil {
			reply(w, nil, errors.New("bad request"))
			return
		}
		pw := strings.ReplaceAll(strings.TrimSpace(req.Password), " ", "") // app passwords are shown in groups of 4
		inKeychain, err := settings.setPassword(strings.TrimSpace(req.User), pw)
		reply(w, map[string]bool{"keychain": inKeychain}, err)
	})
	api("/api/alerts", func(w http.ResponseWriter, r *http.Request) { reply(w, mon.recent(), nil) })
	api("/api/alerts/test-email", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, ml.test())
	})
	api("/api/alerts/test-popup", func(w http.ResponseWriter, r *http.Request) {
		snap := col.last()
		a := Alert{Time: time.Now().UnixMilli(), Rule: "test", Title: "This is a test alert",
			Body: "Real alerts look like this and appear when memory, the processor or the disk need attention."}
		if r.URL.Query().Get("critical") == "1" {
			a.Critical = true
			a.Title = "Test: memory will run out in about 4 minutes"
			for _, p := range snap.Procs {
				if !p.System {
					a.Body = fmt.Sprintf("(Test only.) A real alert would name the app that's growing, e.g. %s, and offer to pause it.", p.Name)
					break
				}
			}
		}
		go pop.show(a)
		reply(w, map[string]bool{"ok": true}, nil)
	})
	api("/api/report", func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		reply(w, rp.build(days), nil)
	})
	api("/api/report/email", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			reply(w, nil, errors.New("POST only"))
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		reply(w, map[string]bool{"ok": true}, ml.report(rp.build(days)))
	})
	api("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bye"))
		quit <- struct{}{}
	})

	srv := &http.Server{Handler: secureHeaders(host, mux), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10}
	go srv.Serve(ln)
	url := dashboardURL(in)
	log.Printf("TaskLens %s running at %s", version, url)
	if !*noBrowser && !*background {
		openBrowser(url)
	}

	exit := func() {
		time.Sleep(200 * time.Millisecond)
		rec.flush()
		cleanup(in)
		os.Exit(0)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt) // the computer is shutting down: save the last few minutes
	go func() {
		tick := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-quit:
				exit()
			case <-stop:
				exit()
			case <-tick.C:
				// In background mode TaskLens keeps watching after the dashboard
				// closes; otherwise it tidies itself away.
				idle := time.Since(time.Unix(0, lastSeen.Load()))
				if !*stay && !settings.get().Background && idle > idleExit {
					log.Printf("no dashboard open for %s; exiting", idle.Round(time.Second))
					exit()
				}
			}
		}
	}()
	if *noTray {
		select {}
	}
	(&trayApp{open: openDash, quit: exit, mon: mon, col: col, settings: settings}).run()
	exit()
}

// secureHeaders adds the browser's own protections to every response: only
// TaskLens's own scripts may run, no other site may frame the page, and the
// page refuses requests addressed to any other host name.
func secureHeaders(host string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; object-src 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func cleanup(in instance) {
	raw, err := os.ReadFile(instanceFile())
	if err == nil && strings.Contains(string(raw), in.Token) {
		os.Remove(instanceFile())
	}
}
