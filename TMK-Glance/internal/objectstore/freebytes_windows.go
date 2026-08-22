//go:build windows

package objectstore

import (
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceExProc = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func (s *Local) FreeBytes() (uint64, error) {
	path, err := syscall.UTF16PtrFromString(s.root)
	if err != nil {
		return 0, err
	}
	var free, total, available uint64
	result, _, callErr := getDiskFreeSpaceExProc.Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&available)))
	if result == 0 {
		return 0, callErr
	}
	return available, nil
}
