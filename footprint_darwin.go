//go:build darwin && cgo

package main

/*
#include <libproc.h>
#include <sys/resource.h>

// phys_footprint is the "Memory" column in Activity Monitor. Unlike RSS it
// includes GPU and compressed memory, which ML jobs use heavily.
static unsigned long long footprint(int pid) {
	struct rusage_info_v4 ri;
	if (proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&ri) != 0) return 0;
	return ri.ri_phys_footprint;
}
*/
import "C"

func physFootprint(pid int32) uint64 { return uint64(C.footprint(C.int(pid))) }
