// Package persistcache persists per-key update timestamps for CLI tasks.
package persistcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Serializer[K comparable] struct {
	Encode func(K) (string, error)
	Decode func(string) (K, error)
}

var String = Serializer[string]{func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil }}
var Int = Serializer[int]{func(n int) (string, error) { return strconv.Itoa(n), nil }, strconv.Atoi}
var Int64 = Serializer[int64]{func(n int64) (string, error) { return strconv.FormatInt(n, 10), nil }, func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }}

func JSON[K comparable]() Serializer[K] {
	return Serializer[K]{func(k K) (string, error) { b, err := json.Marshal(k); return string(b), err }, func(s string) (K, error) { var k K; err := json.Unmarshal([]byte(s), &k); return k, err }}
}

type PersistentCache[K comparable] struct {
	mu         sync.RWMutex
	saveMu     sync.Mutex
	data       map[K]time.Time
	path       string
	serializer Serializer[K]
	notify     chan struct{}
	done       chan struct{}
	closed     bool
	lastErr    error
}

// New starts a coalescing save worker. Close must be called to flush and join it.
// Files are atomic and mode 0600; use one cache owner per path/process.
func New[K comparable](path string, serializer Serializer[K]) (*PersistentCache[K], error) {
	if path == "" || serializer.Encode == nil || serializer.Decode == nil {
		return nil, errors.New("persistcache: path/serializer required")
	}
	c := &PersistentCache[K]{data: map[K]time.Time{}, path: path, serializer: serializer, notify: make(chan struct{}, 1), done: make(chan struct{})}
	f, err := os.Open(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
		if err != nil {
			return nil, err
		}
		if len(b) > 16<<20 {
			return nil, errors.New("persistcache: oversized file")
		}
		var stored map[string]time.Time
		if err = json.Unmarshal(b, &stored); err != nil {
			return nil, errors.New("persistcache: invalid file")
		}
		for text, updated := range stored {
			k, err := serializer.Decode(text)
			if err != nil {
				return nil, fmt.Errorf("persistcache: decode key: %w", err)
			}
			c.data[k] = updated
		}
	}
	go func() {
		defer close(c.done)
		for range c.notify {
			err := c.Save()
			c.mu.Lock()
			c.lastErr = err
			c.mu.Unlock()
		}
	}()
	return c, nil
}
func (c *PersistentCache[K]) changed() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}
func (c *PersistentCache[K]) MarkUpdated(k K) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	c.data[k] = time.Now()
	c.changed()
	return nil
}
func (c *PersistentCache[K]) GetLastUpdate(k K) (time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[k]
	return v, ok
}
func (c *PersistentCache[K]) ShouldUpdate(k K, expire time.Duration) bool {
	v, ok := c.GetLastUpdate(k)
	return !ok || expire <= 0 || time.Since(v) >= expire
}
func (c *PersistentCache[K]) Has(k K) bool { _, ok := c.GetLastUpdate(k); return ok }
func (c *PersistentCache[K]) Delete(k K) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	delete(c.data, k)
	c.changed()
	return nil
}
func (c *PersistentCache[K]) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	c.data = map[K]time.Time{}
	c.changed()
	return nil
}
func (c *PersistentCache[K]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]K, 0, len(c.data))
	for k := range c.data {
		out = append(out, k)
	}
	return out
}
func (c *PersistentCache[K]) Len() int         { c.mu.RLock(); defer c.mu.RUnlock(); return len(c.data) }
func (c *PersistentCache[K]) LastError() error { c.mu.RLock(); defer c.mu.RUnlock(); return c.lastErr }
func (c *PersistentCache[K]) Save() error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	c.mu.RLock()
	stored := map[string]time.Time{}
	for k, v := range c.data {
		text, err := c.serializer.Encode(k)
		if err != nil {
			c.mu.RUnlock()
			return err
		}
		if _, exists := stored[text]; exists {
			c.mu.RUnlock()
			return errors.New("persistcache: serializer key collision")
		}
		stored[text] = v
	}
	c.mu.RUnlock()
	b, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return errors.New("persistcache: cache exceeds file limit")
	}
	if err = os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".persist-*")
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
	return os.Rename(tmp.Name(), c.path)
}
func (c *PersistentCache[K]) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.notify)
	}
	c.mu.Unlock()
	<-c.done
	return c.Save()
}
