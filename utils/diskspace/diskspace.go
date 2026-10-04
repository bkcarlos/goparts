// Package diskspace reports available space on the filesystem containing a path.
package diskspace

import (
	"errors"
	"fmt"
)

var ErrInsufficient = errors.New("diskspace: insufficient available space")

func CheckDiskSpace(path string, required uint64) error {
	available, err := Available(path)
	if err != nil {
		return err
	}
	if available < required {
		return fmt.Errorf("%w: need %d, available %d", ErrInsufficient, required, available)
	}
	return nil
}
