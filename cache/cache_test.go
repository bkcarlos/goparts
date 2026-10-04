package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoresCopiesAndExpiry(t *testing.T) {
	m, _ := NewMemory(2)
	f, _ := NewFile(t.TempDir(), 1024)
	for _, store := range []Store{m, f} {
		ctx := context.Background()
		input := []byte("value")
		if err := store.Set(ctx, "../key", input, time.Hour); err != nil {
			t.Fatal(err)
		}
		input[0] = 'x'
		v, ok, err := store.Get(ctx, "../key")
		if err != nil || !ok || string(v) != "value" {
			t.Fatal(string(v), ok, err)
		}
		v[0] = 'y'
		v, _, _ = store.Get(ctx, "../key")
		if string(v) != "value" {
			t.Fatal("aliased")
		}
		store.Set(ctx, "expired", nil, time.Nanosecond)
		time.Sleep(time.Millisecond)
		if _, ok, _ := store.Get(ctx, "expired"); ok {
			t.Fatal("expired")
		}
	}
}
func TestCoalescedFetch(t *testing.T) {
	m, _ := NewMemory(10)
	l := &Loader{Store: m}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := l.LoadOrFetch(context.Background(), "key", time.Hour, func(context.Context) ([]byte, error) { calls.Add(1); return []byte("yes"), nil })
			if err != nil || string(v) != "yes" {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
