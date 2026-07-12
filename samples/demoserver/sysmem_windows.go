//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// getAvailablePhysicalMemory returns the amount of available physical memory in bytes.
func getAvailablePhysicalMemory() uint64 {
	const fallbackLimit = 10 * 1024 * 1024 * 1024 // 10 GB fallback

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")
	var memInfo memoryStatusEx
	memInfo.cbSize = uint32(unsafe.Sizeof(memInfo))
	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memInfo)))
	if ret != 0 {
		return memInfo.ullAvailPhys
	}
	return fallbackLimit
}
