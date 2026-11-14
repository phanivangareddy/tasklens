package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchLabel = "app.tasklens.agent"

func launchPlist() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
}

// setStartAtLogin adds or removes a LaunchAgent that starts TaskLens quietly
// (no browser window) each time you log in.
func setStartAtLogin(on bool) error {
	if !on {
		exec.Command(sysTool("launchctl"), "remove", launchLabel).Run()
		err := os.Remove(launchPlist())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(exe)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + launchLabel + `</string>
  <key>ProgramArguments</key><array><string>` + esc + `</string><string>--background</string></array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict></plist>
`
	os.MkdirAll(filepath.Dir(launchPlist()), 0o755)
	return os.WriteFile(launchPlist(), []byte(plist), 0o644)
}
