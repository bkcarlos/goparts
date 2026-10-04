//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package diskspace

import "errors"

func Available(string) (uint64, error) { return 0, errors.New("diskspace: unsupported platform") }
