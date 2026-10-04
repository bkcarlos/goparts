package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ComputeFileMD5 is for compatibility checksums, not signatures or security.
func ComputeFileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type DirectoryOptions struct {
	Workers    int
	PutOptions PutOptions
	Filter     func(string, fs.DirEntry) bool
}

// UploadDirectory skips filtered entries and rejects symlinks/special files.
// On failure, already uploaded objects remain; returned results describe them.
func (c *Client) UploadDirectory(ctx context.Context, prefix, root string, opts DirectoryOptions) ([]Object, error) {
	if ctx == nil || opts.Workers < 0 || opts.Workers > 64 {
		return nil, errors.New("storage: invalid directory config")
	}
	if opts.Workers == 0 {
		opts.Workers = 4
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" && !validKey(prefix) {
		return nil, errors.New("storage: invalid prefix")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("storage: source must be a directory")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type file struct{ path, key string }
	jobs := make(chan file)
	var mu sync.Mutex
	var objects []Object
	var failures []error
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				o, err := c.PutFile(ctx, f.key, f.path, opts.PutOptions)
				mu.Lock()
				if err != nil {
					failures = append(failures, err)
					cancel()
				} else {
					objects = append(objects, o)
				}
				mu.Unlock()
			}
		}()
	}
	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if opts.Filter != nil && !opts.Filter(filepath.ToSlash(rel), e) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			return nil
		}
		if !e.Type().IsRegular() {
			return errors.New("storage: directory contains symlink/special file")
		}
		key := filepath.ToSlash(rel)
		if prefix != "" {
			key = prefix + "/" + key
		}
		select {
		case jobs <- file{path, key}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(jobs)
	wg.Wait()
	if err != nil {
		failures = append(failures, err)
	}
	return objects, errors.Join(failures...)
}
