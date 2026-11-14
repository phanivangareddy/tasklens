//go:build !(darwin && cgo)

package main

func physFootprint(int32) uint64 { return 0 }
