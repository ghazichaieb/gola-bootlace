//go:build !windows

// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package library

import "syscall"

// DiskSpace is the free and the total space of the disk that holds path, in bytes.
func DiskSpace(path string) (free, total uint64, err error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, err
	}
	return uint64(s.Bavail) * uint64(s.Bsize), uint64(s.Blocks) * uint64(s.Bsize), nil
}
