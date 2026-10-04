// Package download streams arbitrary sources to files with bounded size,
// checksum verification and publication only after successful completion.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrTooLarge = errors.New("download: size limit exceeded")
var ErrSizeMismatch = errors.New("download: response length mismatch")
var ErrChecksum = errors.New("download: SHA-256 mismatch")
var ErrExists = errors.New("download: destination exists")

type Stream struct {
	Body io.ReadCloser
	Size int64
} // Size=-1 when unknown
type Source interface {
	Open(context.Context) (Stream, error)
}
type SourceFunc func(context.Context) (Stream, error)

func (f SourceFunc) Open(ctx context.Context) (Stream, error) { return f(ctx) }

type Config struct {
	Timeout    time.Duration
	MaxBytes   int64
	BufferSize int
}
type Options struct {
	SHA256     string                           // optional expected lowercase/uppercase hex digest
	Overwrite  bool                             // default false, race-safe no-clobber publication
	OnProgress func(written, total int64) error // total=-1 when unknown; errors abort
}
type Result struct {
	Path   string
	Bytes  int64
	SHA256 string
}
type Client struct{ cfg Config }

func New(cfg Config) (*Client, error) {
	if cfg.Timeout < 0 || cfg.MaxBytes < 0 || cfg.MaxBytes == math.MaxInt64 || cfg.BufferSize < 0 || cfg.BufferSize > 16*1024*1024 {
		return nil, errors.New("download: invalid timeout or size limit")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 10 * 1024 * 1024 * 1024
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 64 * 1024
	}
	return &Client{cfg: cfg}, nil
}

// Fetch owns and closes the opened stream. The destination directory must
// exist. It must not be concurrently modified by an untrusted process.
func (c *Client) Fetch(ctx context.Context, source Source, destination string, opts Options) (result Result, err error) {
	if ctx == nil || source == nil || destination == "" {
		return result, errors.New("download: context, source and destination required")
	}
	if opts.SHA256 != "" {
		digest, e := hex.DecodeString(opts.SHA256)
		if e != nil || len(digest) != sha256.Size {
			return result, errors.New("download: invalid SHA-256")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if info, e := os.Lstat(destination); e == nil {
		if !opts.Overwrite {
			return result, ErrExists
		}
		if !info.Mode().IsRegular() {
			return result, errors.New("download: destination is not a regular file")
		}
	} else if !os.IsNotExist(e) {
		return result, e
	}
	stream, err := source.Open(ctx)
	if err != nil {
		return result, err
	}
	if stream.Body == nil {
		return result, errors.New("download: source returned no body")
	}
	closed := false
	defer func() {
		if !closed {
			stream.Body.Close()
		}
	}()
	if stream.Size < -1 {
		return result, errors.New("download: invalid source size")
	}
	if stream.Size > c.cfg.MaxBytes {
		return result, ErrTooLarge
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".goparts-download-*")
	if err != nil {
		return result, err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	hash := sha256.New()
	buffer := make([]byte, c.cfg.BufferSize)
	var written int64
	emptyReads := 0
	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		// Read at most one byte beyond the cap to detect overflow without a
		// large final read. A partial or unverified file is never published.
		chunk := buffer
		if int64(len(chunk)) > c.cfg.MaxBytes-written+1 {
			chunk = chunk[:c.cfg.MaxBytes-written+1]
		}
		n, readErr := stream.Body.Read(chunk)
		if n > 0 {
			emptyReads = 0
			if int64(n) > c.cfg.MaxBytes-written {
				return result, ErrTooLarge
			}
			if stream.Size >= 0 && int64(n) > stream.Size-written {
				return result, ErrSizeMismatch
			}
			if _, err = f.Write(chunk[:n]); err != nil {
				return result, err
			}
			hash.Write(chunk[:n])
			written += int64(n)
			if opts.OnProgress != nil {
				if err = opts.OnProgress(written, stream.Size); err != nil {
					return result, err
				}
			}
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= 100 {
				return result, io.ErrNoProgress
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return result, readErr
			}
			break
		}
	}
	if stream.Size >= 0 && written != stream.Size {
		return result, ErrSizeMismatch
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if opts.SHA256 != "" && !strings.EqualFold(opts.SHA256, checksum) {
		return result, ErrChecksum
	}
	closed = true
	if err = stream.Body.Close(); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = f.Sync(); err != nil {
		return result, err
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	if opts.Overwrite {
		err = os.Rename(f.Name(), destination)
	} else {
		// Hard-link publication is atomic and fails if another writer won.
		// No unsafe stat-then-rename fallback on filesystems without hard links.
		err = os.Link(f.Name(), destination)
		if os.IsExist(err) {
			err = ErrExists
		}
	}
	if err != nil {
		return result, err
	}
	return Result{Path: destination, Bytes: written, SHA256: checksum}, nil
}
