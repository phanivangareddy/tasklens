package main

import "os"

func allocSize(fi os.FileInfo) int64 { return fi.Size() }

// devOf returns 0 on Windows: drives are separate roots, and junctions are
// reported as non-directories by Lstat, so a scan can't wander onto another drive.
func devOf(os.FileInfo) uint64 { return 0 }
