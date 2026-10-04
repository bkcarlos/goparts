package dedup

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentReplayAndFailedRetry(t *testing.T) {
	d, _ := NewMemory(time.Hour, 10)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := d.Do(context.Background(), "event", func() ([]byte, error) { calls.Add(1); return []byte("response"), nil })
			if err != nil || string(v) != "response" {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	d.Do(context.Background(), "failed", func() ([]byte, error) { return nil, errors.New("transient") })
	v, err := d.Do(context.Background(), "failed", func() ([]byte, error) { return []byte("retry success"), nil })
	if err != nil || string(v) != "retry success" {
		t.Fatal(v, err)
	}
}
