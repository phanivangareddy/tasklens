package main

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a console window flashing up when TaskLens runs a system tool.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
