//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type diskStats struct {
	AvailableBytes int64
	TotalBytes     int64
}

func getDiskStats(path string) (diskStats, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return diskStats{}, err
	}

	var freeBytesAvailable uint64
	var totalBytes uint64
	var totalFreeBytes uint64
	ret, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return diskStats{}, callErr
	}

	return diskStats{
		AvailableBytes: int64(freeBytesAvailable),
		TotalBytes:     int64(totalBytes),
	}, nil
}
