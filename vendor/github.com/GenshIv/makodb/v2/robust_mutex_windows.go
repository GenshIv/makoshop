//go:build windows

package makodb

import (
	"syscall"
)

// isProcessAlive checks if a process with the given PID is still running on Windows.
func isProcessAlive(pid uint32) bool {
	if pid == 0 {
		return false
	}

	// Open process handle with query permissions (0x1000 = PROCESS_QUERY_LIMITED_INFORMATION)
	handle, err := syscall.OpenProcess(0x1000, false, pid)
	if err != nil {
		// ERROR_INVALID_PARAMETER (87) means the PID does not exist (process is dead)
		if err == syscall.Errno(87) {
			return false
		}
		// Any other error (like Access Denied) means the process exists but query is restricted
		return true
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	err = syscall.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return false
	}
	// exitCode 259 is STILL_ACTIVE
	return exitCode == 259
}
