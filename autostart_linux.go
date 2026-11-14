package main

import (
	"os"
	"path/filepath"
)

func autostartFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "tasklens.desktop")
}

// setStartAtLogin uses the freedesktop autostart folder, which GNOME, KDE,
// Xfce and Cinnamon all read at login.
func setStartAtLogin(on bool) error {
	if !on {
		err := os.Remove(autostartFile())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=TaskLens\nComment=Watches memory, processor and disk, and warns you early\n" +
		"Exec=\"" + exe + "\" --background\nIcon=tasklens\nX-GNOME-Autostart-enabled=true\nNoDisplay=true\n"
	os.MkdirAll(filepath.Dir(autostartFile()), 0o755)
	return os.WriteFile(autostartFile(), []byte(entry), 0o644)
}
