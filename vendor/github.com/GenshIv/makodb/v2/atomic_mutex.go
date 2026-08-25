package makodb

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"unsafe"
)

const (
	atomicMaxSpins     = 2000
	atomicMaxYields    = 200
	atomicBackoffStart = 1
	atomicBackoffMax   = 16
)

// AtomicShmMutex is a shared-memory write lock backed by a separate memory-mapped
// lock file. Fully autonomous: independent of DB shard files, unaffected by
// compaction/vacuum.
//
// Each lock uses 1 bit:
//   - bit 0: write lock (Put/Delete/resize/Shrink/close via RobustShmMutex)
//
// Rules:
//   - Writers: CAS free→locked, work, store free.
//
// Not robust: if a process crashes, locks are NOT recovered.
// This is intentional: the mutex is used only for short-lived operations,
// and the process is expected to exit cleanly.
type AtomicShmMutex struct {
	path   string
	file   *os.File
	mapped []byte
}

// OpenAtomicMutex opens or creates the lock file for a ShardedDB.
// numLocks is the number of independent locks (typically = numShards).
// The lock file is stored as <dirPath>/lock.state.
func OpenAtomicMutex(dirPath string, numLocks int) (*AtomicShmMutex, error) {
	if numLocks <= 0 {
		numLocks = 1
	}

	lockPath := filepath.Join(dirPath, "lock.state")

	fileSize := 4 + numLocks*4 // [numLocks: uint32][lock0: uint32][lock1: uint32]...

	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if info.Size() < int64(fileSize) {
		if err := f.Truncate(int64(fileSize)); err != nil {
			f.Close()
			return nil, err
		}
	}

	mapped, err := mmapFileRW(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	am := &AtomicShmMutex{
		path:   lockPath,
		file:   f,
		mapped: mapped,
	}

	storedNumLocks := atomic.LoadUint32((*uint32)(unsafe.Pointer(&mapped[0])))
	if storedNumLocks == 0 || int(storedNumLocks) != numLocks {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&mapped[0])), uint32(numLocks))
		for i := 0; i < numLocks; i++ {
			atomic.StoreUint32(am.lockPtr(i), 0)
		}
	}

	return am, nil
}

// Close closes the atomic mutex file.
func (am *AtomicShmMutex) Close() error {
	var errs []error
	if am.mapped != nil {
		if err := unmapFile(am.mapped); err != nil {
			errs = append(errs, err)
		}
	}
	if am.file != nil {
		if err := am.file.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (am *AtomicShmMutex) lockPtr(idx int) *uint32 {
	return (*uint32)(unsafe.Pointer(&am.mapped[4+idx*4]))
}

// Lock acquires the write lock.
// Must be released with Unlock.
func (am *AtomicShmMutex) Lock(idx int) {
	ptr := am.lockPtr(idx)
	spins := 0
	yields := 0
	backoff := atomicBackoffStart

	for {
		if atomic.CompareAndSwapUint32(ptr, 0, 1) {
			return
		}

		if spins < atomicMaxSpins {
			spins++
		} else if yields < atomicMaxYields {
			runtime.Gosched()
			yields++
		} else {
			for i := 0; i < backoff; i++ {
				runtime.Gosched()
			}
			if backoff < atomicBackoffMax {
				backoff <<= 1
			}
		}
	}
}

// Unlock releases the write lock.
func (am *AtomicShmMutex) Unlock(idx int) {
	ptr := am.lockPtr(idx)
	atomic.StoreUint32(ptr, 0)
}

// mmapFileRW memory-maps a file for read-write access.
func mmapFileRW(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := int(info.Size())
	if size <= 0 {
		return nil, os.ErrInvalid
	}
	return mmapSyscall(f, size)
}

// unmapFile unmaps a memory-mapped region.
func unmapFile(data []byte) error {
	return munmapSyscall(data)
}
