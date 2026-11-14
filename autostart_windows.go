package main

import "golang.org/x/sys/windows/registry"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// setStartAtLogin adds TaskLens to the current user's startup apps (the same
// list Task Manager → Startup apps shows), starting quietly in the tray.
func setStartAtLogin(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("TaskLens"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	return k.SetStringValue("TaskLens", `"`+exe+`" --background`)
}
