//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package diskspace

import (
	"math"
	"syscall"
)

func Available(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	blocks, size := uint64(stat.Bavail), uint64(stat.Bsize)
	if size > 0 && blocks > math.MaxUint64/size {
		return math.MaxUint64, nil
	}
	return blocks * size, nil
}
