//go:build !windows

package objectstore

import "syscall"

func (s *Local) FreeBytes() (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
