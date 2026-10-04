//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly || windows

package diskspace

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
)

func TestCapacityChecks(t *testing.T) {
	dir := t.TempDir()
	if _, err := Available(dir); err != nil {
		t.Fatal(err)
	}
	if err := CheckDiskSpace(dir, 0); err != nil {
		t.Fatal(err)
	}
	if err := CheckDiskSpace(dir, math.MaxUint64); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient space: %v", err)
	}
	if err := CheckDiskSpace(filepath.Join(dir, "missing"), 0); err == nil || errors.Is(err, ErrInsufficient) {
		t.Fatalf("filesystem error lost: %v", err)
	}
}
