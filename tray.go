package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"time"

	"fyne.io/systray"
)

//go:embed packaging/tray-template.png
var trayTemplate []byte

//go:embed packaging/tray.png
var trayPNG []byte

//go:embed packaging/tasklens.ico
var trayICO []byte

type trayApp struct {
	open     func()
	quit     func()
	mon      *monitor
	col      *collector
	settings *settingsStore
}

// run shows the menu bar (macOS) / notification area (Windows, Linux) icon.
// It must be called on the main goroutine and blocks until quit.
func (t *trayApp) run() {
	systray.Run(t.ready, func() {})
}

func (t *trayApp) ready() {
	switch runtime.GOOS {
	case "windows":
		systray.SetIcon(trayICO)
	case "darwin":
		systray.SetTemplateIcon(trayTemplate, trayTemplate)
	default:
		systray.SetIcon(trayPNG)
	}
	systray.SetTooltip("TaskLens")
	status := systray.AddMenuItem("Measuring…", "")
	status.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("Open TaskLens", "Show the dashboard")
	mute := systray.AddMenuItem("Pause alerts for 1 hour", "No pop-ups or emails for an hour")
	login := systray.AddMenuItemCheckbox("Start at login", "Watch this computer from the moment you log in", t.settings.get().StartAtLogin)
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit TaskLens", "Stop watching and close")
	systray.SetOnTapped(t.open) // Windows/Linux: a click on the icon opens the dashboard

	go func() {
		for range time.Tick(5 * time.Second) {
			s := t.col.last().System
			if s.MemTotal == 0 {
				continue
			}
			line := fmt.Sprintf("Processor %.0f%% · Memory %.0f%% · Disk %s free",
				s.CPU, float64(s.MemUsed)/float64(s.MemTotal)*100, human(s.DiskFree))
			if t.mon.muted() {
				line += " · alerts paused"
			}
			status.SetTitle(line)
			systray.SetTooltip("TaskLens: " + line)
		}
	}()
	go func() {
		for {
			select {
			case <-open.ClickedCh:
				t.open()
			case <-mute.ClickedCh:
				if t.mon.muted() {
					t.mon.mute(0)
					mute.SetTitle("Pause alerts for 1 hour")
				} else {
					t.mon.mute(time.Hour)
					mute.SetTitle("Resume alerts")
				}
			case <-login.ClickedCh:
				s := t.settings.get()
				s.StartAtLogin = !login.Checked()
				if err := setStartAtLogin(s.StartAtLogin); err == nil && t.settings.save(s) == nil {
					if s.StartAtLogin {
						login.Check()
					} else {
						login.Uncheck()
					}
				}
			case <-quit.ClickedCh:
				t.quit()
			}
		}
	}()
}
