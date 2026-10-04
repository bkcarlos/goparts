// Package utils provides small formatting and archive helpers.
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

func FormatDuration(d time.Duration) string { return d.Round(time.Millisecond).String() }
func FormatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	for _, unit := range units {
		v /= 1024
		if v < 1024 || unit == "EiB" {
			return fmt.Sprintf("%.2f %s", v, unit)
		}
	}
	return ""
}

// GetVersionHash returns a stable short SHA-256 identifier for build metadata.
func GetVersionHash(version string) string {
	sum := sha256.Sum256([]byte(version))
	return hex.EncodeToString(sum[:8])
}
func GetEnvOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
