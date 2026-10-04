package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

type File struct {
	dir      string
	maxBytes int64
}
type fileEntry struct {
	Value   []byte    `json:"value"`
	Expires time.Time `json:"expires"`
}

func NewFile(dir string, maxBytes int64) (*File, error) {
	if dir == "" || maxBytes < 0 || maxBytes > 1<<30 {
		return nil, errors.New("cache: invalid file config")
	}
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &File{dir, maxBytes}, nil
}
func (f *File) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(f.dir, hex.EncodeToString(sum[:])+".json")
}
func (f *File) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	file, err := os.Open(f.path(key))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, f.maxBytes*2+1024))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) >= f.maxBytes*2+1024 {
		return nil, false, errors.New("cache: oversized entry")
	}
	var e fileEntry
	if json.Unmarshal(b, &e) != nil || int64(len(e.Value)) > f.maxBytes {
		return nil, false, errors.New("cache: invalid entry")
	}
	if !e.Expires.IsZero() && !time.Now().Before(e.Expires) {
		return nil, false, nil
	}
	return e.Value, true, nil
}
func (f *File) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl < 0 || int64(len(value)) > f.maxBytes {
		return errors.New("cache: invalid TTL/value size")
	}
	e := fileEntry{Value: value}
	if ttl > 0 {
		e.Expires = time.Now().Add(ttl)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, ".cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path(key))
}
func (f *File) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(f.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
