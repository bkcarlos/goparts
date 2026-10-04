package dedup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInFlightCapacityCancellationAndReplayIsolation(t *testing.T) {
	m, err := NewMemory(time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	result := make(chan error, 1)
	original := []byte("response")
	go func() {
		v, err := m.Do(context.Background(), "key", func() ([]byte, error) { close(entered); <-release; return original, nil })
		if err == nil && string(v) != "response" {
			err = errors.New("wrong response")
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not started")
	}
	if _, err := m.Do(context.Background(), "other", func() ([]byte, error) { t.Error("capacity bypassed"); return nil, nil }); err == nil {
		t.Fatal("capacity exceeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() {
		_, err := m.Do(ctx, "key", func() ([]byte, error) { t.Error("duplicate handler ran"); return nil, nil })
		waiting <- err
	}()
	cancel()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter stuck")
	}
	unblock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler stuck")
	}
	original[0] = 'X'
	for i := 0; i < 2; i++ {
		v, err := m.Do(context.Background(), "key", func() ([]byte, error) { t.Error("successful result not cached"); return nil, nil })
		if err != nil || string(v) != "response" {
			t.Fatalf("response aliased: %q %v", v, err)
		}
		v[0] = 'Y'
	}
	// Advance the stored expiry without a wall-clock sleep.
	m.mu.Lock()
	m.entries["key"].expires = time.Now().Add(-time.Second)
	m.mu.Unlock()
	v, err := m.Do(context.Background(), "other", func() ([]byte, error) { return []byte("new"), nil })
	if err != nil || string(v) != "new" {
		t.Fatalf("expired entry retained capacity: %q %v", v, err)
	}
}

func TestPanicAndCanceledOwnerCanBeRetried(t *testing.T) {
	for _, kind := range []string{"panic", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			m, err := NewMemory(time.Hour, 1)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err = m.Do(ctx, "key", func() ([]byte, error) {
				if kind == "panic" {
					panic("private")
				}
				cancel()
				return []byte("partial"), nil
			})
			if err == nil {
				t.Fatal("failed execution accepted")
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			v, err := m.Do(context.Background(), "key", func() ([]byte, error) { return []byte("retry"), nil })
			if err != nil || string(v) != "retry" {
				t.Fatalf("failure poisoned retry: %q %v", v, err)
			}
		})
	}
}
