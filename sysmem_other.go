//go:build !windows

package main

func kernelMemory() uint64 { return 0 }
