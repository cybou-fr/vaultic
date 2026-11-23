//go:build !windows

package main

import "os"

// soakHandleCount returns the number of open file descriptors of this process.
func soakHandleCount() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return len(entries)
}
