// Package dedup coalesces event handlers and replays successful response bytes.
package dedup

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Store implementations must atomically execute at most one fn per key, cache
// only successes, and preserve returned response bytes for redelivery.
type Store interface {
	Do(context.Context, string, func() ([]byte, error)) ([]byte, error)
}
type entry struct {
	done    chan struct{}
	value   []byte
	err     error
	expires time.Time
}
type Memory struct {
	mu       sync.Mutex
	entries  map[string]*entry
	ttl      time.Duration
	capacity int
}

func NewMemory(ttl time.Duration, capacity int) (*Memory, error) {
	if ttl <= 0 || capacity < 1 {
		return nil, errors.New("dedup: positive TTL/capacity required")
	}
	return &Memory{entries: map[string]*entry{}, ttl: ttl, capacity: capacity}, nil
}
func (m *Memory) Do(ctx context.Context, key string, fn func() ([]byte, error)) ([]byte, error) {
	if ctx == nil || key == "" || fn == nil {
		return nil, errors.New("dedup: context/key/function required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	now := time.Now()
	for k, e := range m.entries {
		if !e.expires.IsZero() && !now.Before(e.expires) {
			delete(m.entries, k)
		}
	}
	if e, ok := m.entries[key]; ok {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.done:
			return append([]byte(nil), e.value...), e.err
		}
	}
	if len(m.entries) >= m.capacity {
		m.mu.Unlock()
		return nil, errors.New("dedup: capacity exhausted")
	}
	e := &entry{done: make(chan struct{})}
	m.entries[key] = e
	m.mu.Unlock()
	func() {
		defer func() {
			if recover() != nil {
				e.err = errors.New("dedup: handler panicked")
			}
		}()
		e.value, e.err = fn()
		if e.err == nil {
			e.err = ctx.Err()
		}
		e.value = append([]byte(nil), e.value...)
	}()
	m.mu.Lock()
	if e.err != nil {
		delete(m.entries, key)
	} else {
		e.expires = time.Now().Add(m.ttl)
	}
	close(e.done)
	m.mu.Unlock()
	return append([]byte(nil), e.value...), e.err
}
