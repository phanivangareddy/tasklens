//go:build !windows

package main

import (
	"os"
	"syscall"
)

// allocSize is the space a file really takes on disk (like du), so sparse
// files and APFS/ext4 block rounding are counted correctly.
func allocSize(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return fi.Size()
}

// devOf identifies the file system, so a scan stays on one disk.
func devOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)
	}
	return 0
}
