//go:build windows

package makodb

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32           = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileMapping = modkernel32.NewProc("CreateFileMappingW")
	procMapViewOfFile     = modkernel32.NewProc("MapViewOfFile")
	procUnmapViewOfFile   = modkernel32.NewProc("UnmapViewOfFile")
	procCloseHandle       = modkernel32.NewProc("CloseHandle")
	procFlushViewOfFile   = modkernel32.NewProc("FlushViewOfFile")
)

const (
	FILE_MAP_ALL_ACCESS = 0x001F
	PAGE_READWRITE      = 0x04
)

func mmapSyscall(f *os.File, size int) ([]byte, error) {
	h, _, err := procCreateFileMapping.Call(
		uintptr(f.Fd()),
		0,
		PAGE_READWRITE,
		uintptr(size>>32),
		uintptr(size),
		0,
	)
	if h == 0 {
		return nil, err
	}

	addr, _, err := procMapViewOfFile.Call(
		h,
		FILE_MAP_ALL_ACCESS,
		0,
		0,
		uintptr(size),
	)
	if addr == 0 {
		procCloseHandle.Call(h)
		return nil, err
	}

	// Store handle in file's SyscallConn for cleanup
	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), size)
	return data, nil
}

func munmapSyscall(data []byte) error {
	ret, _, err := procUnmapViewOfFile.Call(uintptr(unsafe.Pointer(&data[0])))
	if ret == 0 {
		return err
	}
	return nil
}

// madviseSyscall is a no-op on Windows; provided for API symmetry.
func madviseSyscall(data []byte, advice int) error {
	return nil
}

// madviseDontNeedSyscall is a no-op on Windows; provided for API symmetry.
func madviseDontNeedSyscall(data []byte) error {
	return nil
}

// madviseRandomSyscall is a no-op on Windows; provided for API symmetry.
func madviseRandomSyscall(data []byte) error {
	return nil
}

// msyncSyscall synchronizes a memory-mapped file region with the underlying file.
// On Windows, FlushViewOfFile is used to ensure data is written to disk.
func msyncSyscall(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	// On Windows, we need to use FlushViewOfFile
	ret, _, err := procFlushViewOfFile.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	if ret == 0 {
		return err
	}
	return nil
}
