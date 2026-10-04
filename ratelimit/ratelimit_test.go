package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBucketAndCancel(t *testing.T) {
	b, _ := New(0.1, 1)
	if !b.Allow() || b.Allow() {
		t.Fatal("burst")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(b.Wait(ctx), context.Canceled) {
		t.Fatal("cancel")
	}
}
func TestBreakerThreeStates(t *testing.T) {
	b, _ := NewCircuitBreaker(BreakerConfig{Failures: 1, OpenTimeout: time.Millisecond})
	boom := errors.New("boom")
	b.Do(context.Background(), func(context.Context) error { return boom })
	if b.State() != Open {
		t.Fatal(b.State())
	}
	if !errors.Is(b.Do(context.Background(), func(context.Context) error { return nil }), ErrOpen) {
		t.Fatal("open")
	}
	b.mu.Lock()
	b.until = time.Now().Add(-time.Second)
	b.mu.Unlock()
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.Do(context.Background(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	if b.State() != HalfOpen {
		t.Fatal("half open")
	}
	if !errors.Is(b.Do(context.Background(), func(context.Context) error { return nil }), ErrOpen) {
		t.Fatal("parallel probe")
	}
	close(release)
	wg.Wait()
	if b.State() != Closed {
		t.Fatal("closed")
	}
}
