//go:build !windows

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
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

	// Try reading /proc/meminfo for Linux
	file, err := os.Open("/proc/meminfo")
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					val, err := strconv.ParseUint(fields[1], 10, 64)
					if err == nil {
						return val * 1024 // /proc/meminfo values are in kB
					}
				}
			}
		}
	}
	return fallbackLimit
}
