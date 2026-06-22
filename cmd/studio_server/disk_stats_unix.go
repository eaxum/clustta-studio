//go:build !windows

package main

import "syscall"

type diskStats struct {
	AvailableBytes int64
	TotalBytes     int64
}

func getDiskStats(path string) (diskStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskStats{}, err
	}

	return diskStats{
		AvailableBytes: int64(stat.Bavail) * int64(stat.Bsize),
		TotalBytes:     int64(stat.Blocks) * int64(stat.Bsize),
	}, nil
}
