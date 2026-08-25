package makodb

import (
	"os"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	maxSpins     = 1000
	maxYields    = 100
	maxSleepTime = 10 * time.Millisecond
)

// RobustShmMutex is a shared-memory mutex that recovers automatically if a process crashes while holding it.
// It lives inside the mmap region and uses a fixed offset.
// The mapping is never unmapped/remapped during the lifetime of the DB,
// so this mutex always sees valid memory.
type RobustShmMutex struct {
	state *uint32 // direct pointer into mmap region (never changes)
}

// NewRobustShmMutex creates a new RobustShmMutex at the given offset in the mapped slice.
// The slice must remain valid for the lifetime of the mutex.
func NewRobustShmMutex(mapped []byte, offset uintptr) *RobustShmMutex {
	if offset+unsafe.Sizeof(uint32(0)) > uintptr(len(mapped)) {
		panic("makodb: offset out of bounds for shared memory segment")
	}
	return &RobustShmMutex{
		state: (*uint32)(unsafe.Pointer(&mapped[offset])),
	}
}

// Lock acquires the mutex. If another process crashed while holding the lock, it recovers it.
func (m *RobustShmMutex) Lock() {
	myPid := uint32(os.Getpid())
	spins := 0
	yields := 0
	sleepTime := time.Microsecond

	for {
		// 1. Attempt to acquire the lock if it's free (0)
		if atomic.CompareAndSwapUint32(m.state, 0, myPid) {
			return
		}

		// 2. If it is already locked, check the owner PID
		ownerPid := atomic.LoadUint32(m.state)
		if ownerPid != 0 && ownerPid != myPid {
			// Check if the owner process is still alive on the system
			if !isProcessAlive(ownerPid) {
				// The owner process has died while holding the lock!
				// Attempt to steal the lock by swapping ownerPid -> myPid
				if atomic.CompareAndSwapUint32(m.state, ownerPid, myPid) {
					// Successfully recovered the lock!
					return
				}
			}
		}

		// Backoff strategy to avoid CPU burning
		if spins < maxSpins {
			spins++
		} else if yields < maxYields {
			runtime.Gosched()
			yields++
		} else {
			time.Sleep(sleepTime)
			sleepTime *= 2
			if sleepTime > maxSleepTime {
				sleepTime = maxSleepTime
			}
			spins = 0
			yields = 0
		}
	}
}

// Unlock releases the mutex.
func (m *RobustShmMutex) Unlock() {
	myPid := uint32(os.Getpid())
	if !atomic.CompareAndSwapUint32(m.state, myPid, 0) {
		panic("makodb: unlock of mutex not owned by this process")
	}
}
