// Package cache defines a byte cache with TTL and coalesced loading.
package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Store interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}
type Event struct {
	Operation string
	Hit       bool
	Duration  time.Duration
	Err       error
}
type entry struct {
	value            []byte
	expires, created time.Time
}
type Memory struct {
	mu      sync.Mutex
	entries map[string]entry
	max     int
	observe func(Event)
}

func NewMemory(maxEntries int, observer ...func(Event)) (*Memory, error) {
	if maxEntries == 0 {
		maxEntries = 1024
	}
	if maxEntries < 1 {
		return nil, errors.New("cache: invalid capacity")
	}
	m := &Memory{entries: map[string]entry{}, max: maxEntries}
	if len(observer) > 0 {
		m.observe = observer[0]
	}
	return m, nil
}
func (m *Memory) Get(ctx context.Context, key string) (value []byte, hit bool, err error) {
	start := time.Now()
	if m.observe != nil {
		defer func() { m.observe(Event{"get", hit, time.Since(start), err}) }()
	}
	if err = ctx.Err(); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return nil, false, nil
	}
	if !e.expires.IsZero() && !time.Now().Before(e.expires) {
		delete(m.entries, key)
		return nil, false, nil
	}
	return append([]byte(nil), e.value...), true, nil
}
func (m *Memory) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl < 0 {
		return errors.New("cache: negative TTL")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if _, ok := m.entries[key]; !ok && len(m.entries) >= m.max {
		var oldest string
		var when time.Time
		for k, e := range m.entries {
			if when.IsZero() || e.created.Before(when) {
				oldest, when = k, e.created
			}
		}
		delete(m.entries, oldest)
	}
	e := entry{value: append([]byte(nil), value...), created: now}
	if ttl > 0 {
		e.expires = now.Add(ttl)
	}
	m.entries[key] = e
	return nil
}
func (m *Memory) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
	return nil
}

type flight struct {
	done  chan struct{}
	value []byte
	err   error
}
type Loader struct {
	Store   Store
	mu      sync.Mutex
	flights map[string]*flight
}

// LoadOrFetch shares concurrent misses in this Loader. The initiating caller's
// context governs fetch; waiting callers may cancel independently. Fetch must not
// recursively request the same key. Returned slices are independent copies.
func (l *Loader) LoadOrFetch(ctx context.Context, key string, ttl time.Duration, fetch func(context.Context) ([]byte, error)) ([]byte, error) {
	if l.Store == nil || ctx == nil || fetch == nil {
		return nil, errors.New("cache: store/context/fetch required")
	}
	v, ok, err := l.Store.Get(ctx, key)
	if err != nil || ok {
		return v, err
	}
	l.mu.Lock()
	if f, ok := l.flights[key]; ok {
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return append([]byte(nil), f.value...), f.err
		}
	}
	if l.flights == nil {
		l.flights = map[string]*flight{}
	}
	f := &flight{done: make(chan struct{})}
	l.flights[key] = f
	l.mu.Unlock()
	defer func() { l.mu.Lock(); delete(l.flights, key); close(f.done); l.mu.Unlock() }()
	func() {
		defer func() {
			if recover() != nil {
				f.err = errors.New("cache: fetch panicked")
			}
		}()
		f.value, ok, f.err = l.Store.Get(ctx, key)
		if f.err == nil && !ok {
			f.value, f.err = fetch(ctx)
			if f.err == nil {
				f.value = append([]byte(nil), f.value...)
				f.err = l.Store.Set(ctx, key, f.value, ttl)
			}
		}
	}()
	return append([]byte(nil), f.value...), f.err
}
