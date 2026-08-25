package makodb

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// ErrDBLocked is returned when another process already holds the DB lock.
var ErrDBLocked = fmt.Errorf("makodb: database is locked by another process")

// dbLockFile is the name of the lock file inside the DB directory.
const dbLockFileName = ".makodb.lock"

// dbLock represents an exclusive lock on the database directory.
// It uses a lock file with O_EXCL to ensure only one process can open the DB.
// The lock file contains the PID of the holding process.
// On Unix, the file descriptor is kept open for the lifetime of the DB.
// The lock is automatically released when the process exits (fd closed).
type dbLock struct {
	path string
	fd   *os.File
	pid  int
}

// acquireDBLock creates an exclusive lock file in the DB directory.
// Returns ErrDBLocked if another process already holds the lock.
// The lock is held until releaseDBLock is called or the process exits.
func acquireDBLock(dirPath string) (*dbLock, error) {
	lockPath := filepath.Join(dirPath, dbLockFileName)

	// Try to create the lock file exclusively.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		// We created the file — write our PID.
		pid := os.Getpid()
		_, _ = f.WriteString(strconv.Itoa(pid))
		_ = f.Sync()
		return &dbLock{path: lockPath, fd: f, pid: pid}, nil
	}

	if !os.IsExist(err) {
		return nil, fmt.Errorf("makodb: failed to create lock file: %w", err)
	}

	// File exists — check if the holding process is still alive.
	existing, err := os.ReadFile(lockPath)
	if err != nil {
		// Can't read — assume stale, try to remove and re-create.
		_ = os.Remove(lockPath)
		f, err2 := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err2 != nil {
			return nil, ErrDBLocked
		}
		pid := os.Getpid()
		_, _ = f.WriteString(strconv.Itoa(pid))
		_ = f.Sync()
		return &dbLock{path: lockPath, fd: f, pid: pid}, nil
	}

	// Parse PID from existing lock file.
	var oldPID int
	if _, err := fmt.Sscanf(string(existing), "%d", &oldPID); err == nil && oldPID > 0 {
		// Check if the process is still alive by sending signal 0.
		proc, err := os.FindProcess(oldPID)
		if err == nil {
			if err := proc.Signal(syscall.Signal(0)); err == nil {
				// Process is alive — DB is locked.
				return nil, fmt.Errorf("%w (pid %d, lock file %s)", ErrDBLocked, oldPID, lockPath)
			}
		}
		// Process is dead or can't check — stale lock, remove and re-create.
		_ = os.Remove(lockPath)
	} else {
		// Can't parse PID — assume stale.
		_ = os.Remove(lockPath)
	}

	// Re-create the lock file.
	f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrDBLocked
		}
		return nil, fmt.Errorf("makodb: failed to create lock file: %w", err)
	}
	pid := os.Getpid()
	_, _ = f.WriteString(strconv.Itoa(pid))
	_ = f.Sync()
	return &dbLock{path: lockPath, fd: f, pid: pid}, nil
}

// releaseDBLock removes the lock file and closes the fd.
func releaseDBLock(lock *dbLock) {
	if lock == nil {
		return
	}
	_ = lock.fd.Close()
	_ = os.Remove(lock.path)
}
