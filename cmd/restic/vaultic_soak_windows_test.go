package main

import (
	"syscall"
	"unsafe"
)

// soakHandleCount returns the number of open kernel handles of this process.
func soakHandleCount() int {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	self, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	var n uint32
	if r, _, _ := proc.Call(uintptr(self), uintptr(unsafe.Pointer(&n))); r == 0 {
		return 0
	}
	return int(n)
}
