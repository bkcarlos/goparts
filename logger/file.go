package logger

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// FileConfig bounds the active file and keeps Backups numbered historical files.
// Each Write is atomic relative to rotation. A single oversized record is rejected.
type FileConfig struct {
	Path     string
	MaxBytes int64
	Backups  int
}
type RotatingWriter struct {
	mu     sync.Mutex
	cfg    FileConfig
	file   *os.File
	size   int64
	closed bool
}

func NewRotatingWriter(cfg FileConfig) (*RotatingWriter, error) {
	if cfg.Path == "" || cfg.MaxBytes < 0 || cfg.Backups < 0 {
		return nil, errors.New("logger: invalid file config")
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 10 << 20
	}
	w := &RotatingWriter{cfg: cfg}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	s, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = s.Size()
	return nil
}
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if int64(len(p)) > w.cfg.MaxBytes {
		return 0, errors.New("logger: record exceeds file size limit")
	}
	if w.size+int64(len(p)) > w.cfg.MaxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}
func (w *RotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		w.closed = true
		return err
	}
	fail := func(err error) error { w.closed = true; return err }
	for i := w.cfg.Backups; i >= 1; i-- {
		dst := fmt.Sprintf("%s.%d", w.cfg.Path, i)
		src := w.cfg.Path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", w.cfg.Path, i-1)
		}
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return fail(err)
		}
	}
	if w.cfg.Backups == 0 {
		if err := os.Remove(w.cfg.Path); err != nil && !os.IsNotExist(err) {
			return fail(err)
		}
	}
	if err := w.open(); err != nil {
		return fail(err)
	}
	return nil
}
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.file.Close()
}
