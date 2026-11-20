package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetPerformanceInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetPerformanceInfo")

// performanceInformation is PERFORMANCE_INFORMATION from psapi.h.
type performanceInformation struct {
	cb                uint32
	commitTotal       uintptr
	commitLimit       uintptr
	commitPeak        uintptr
	physicalTotal     uintptr
	physicalAvailable uintptr
	systemCache       uintptr
	kernelTotal       uintptr
	kernelPaged       uintptr
	kernelNonpaged    uintptr
	pageSize          uintptr
	handleCount       uint32
	processCount      uint32
	threadCount       uint32
}

// kernelMemory is the memory Windows itself holds for the kernel and drivers
// (paged + non-paged pool). No process owns it, so it never shows in the list.
func kernelMemory() uint64 {
	var pi performanceInformation
	pi.cb = uint32(unsafe.Sizeof(pi))
	if r, _, _ := procGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&pi)), uintptr(pi.cb)); r == 0 {
		return 0
	}
	return uint64(pi.kernelTotal) * uint64(pi.pageSize)
}
