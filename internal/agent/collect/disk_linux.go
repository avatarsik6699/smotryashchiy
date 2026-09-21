//go:build linux

package collect

import "syscall"

func statfs(path string) (FSStat, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return FSStat{}, err
	}
	return FSStat{BlockSize: uint64(s.Bsize), Blocks: uint64(s.Blocks), Free: uint64(s.Bfree), Avail: uint64(s.Bavail)}, nil
}
