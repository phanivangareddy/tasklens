package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// sysTool returns the full path of a system program, so a look-alike placed
// earlier on the PATH (or in the current folder) is never run instead.
func sysTool(name string) string {
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		if name == "explorer" {
			return filepath.Join(root, "explorer.exe")
		}
		dirs = []string{filepath.Join(root, "System32")}
		name += ".exe"
	case "darwin":
		dirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	default:
		dirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/usr/local/bin"}
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil { // unusual layouts (e.g. NixOS)
		return p
	}
	return name
}
