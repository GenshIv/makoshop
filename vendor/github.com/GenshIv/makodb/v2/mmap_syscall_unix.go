//go:build !windows

package makodb

import (
	"os"
	"syscall"
)

func mmapSyscall(f *os.File, size int) ([]byte, error) {
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func munmapSyscall(data []byte) error {
	return syscall.Munmap(data)
}

// madviseSyscall applies an advice to the given memory region.
// On Unix it uses madvise(2); on other platforms it is a no-op.
func madviseSyscall(data []byte, advice int) error {
	if len(data) == 0 {
		return nil
	}
	return syscall.Madvise(data, advice)
}

// madviseDontNeedSyscall tells the kernel that pages in [data] are no longer
// needed; they can be released from memory.
func madviseDontNeedSyscall(data []byte) error {
	return madviseSyscall(data, syscall.MADV_DONTNEED)
}

// madviseRandomSyscall tells the kernel that pages in [data] will be accessed
// in a random order; it may disable readahead optimizations.
func madviseRandomSyscall(data []byte) error {
	return madviseSyscall(data, syscall.MADV_RANDOM)
}

// msyncSyscall synchronizes a memory-mapped file region with the underlying file.
// On Linux, this is a no-op because mmap writes are automatically flushed.
// On macOS/BSD, we would use msync, but Go doesn't expose it directly.
func msyncSyscall(data []byte) error {
	// On most Unix systems, mmap writes are automatically flushed to disk
	// when the file is closed or the system syncs. We can rely on file.Sync()
	// to ensure data persistence.
	return nil
}
