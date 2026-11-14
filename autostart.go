package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func hostname() (string, error) { return os.Hostname() }

// exePath is the real location of the running TaskLens, for start-at-login.
func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	// macOS runs apps opened straight from a .dmg or Downloads from a temporary copy.
	if strings.Contains(p, "/AppTranslocation/") || strings.HasPrefix(p, "/Volumes/") {
		return "", errors.New("move TaskLens into your Applications folder first, then open it from there and turn this on again")
	}
	return p, nil
}
