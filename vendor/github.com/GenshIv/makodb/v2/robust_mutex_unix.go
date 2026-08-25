//go:build !windows

package makodb

import (
	"os"
	"syscall"
)

// isProcessAlive checks if a process with the given PID is still running on Unix.
func isProcessAlive(pid uint32) bool {
	if pid == 0 {
		return false
	}

	process, err := os.FindProcess(int(pid))
	if err != nil {
		return false
	}

	// Sending signal 0 does not terminate the process, but performs error checking.
	// If it returns nil, the process is running.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
