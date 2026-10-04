package ratelimit

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentBucketNeverExceedsBurst(t *testing.T) {
	b, err := New(1e-9, 7)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var allowed atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if b.Allow() {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed.Load() != 7 {
		t.Fatalf("allowed %d, want 7", allowed.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Wait(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not cancel")
	}
}

func TestBucketRejectsInvalidConfiguration(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := New(rate, 1); err == nil {
			t.Errorf("accepted rate %v", rate)
		}
	}
	if _, err := New(1, 0); err == nil {
		t.Fatal("accepted zero burst")
	}
	b, err := New(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(nil); err == nil {
		t.Fatal("accepted nil context")
	}
}

func TestBreakerIgnoresStaleInFlightSuccess(t *testing.T) {
	b, err := NewCircuitBreaker(BreakerConfig{Failures: 1, OpenTimeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() { done <- b.Do(ctx, func(context.Context) error { close(entered); <-release; return nil }) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	want := errors.New("down")
	if err := b.Do(ctx, func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if b.State() != Open {
		t.Fatalf("stale success reset breaker: %s", b.State())
	}
}

func TestBreakerFailureClassificationAndProbeFailure(t *testing.T) {
	ignored, failure := errors.New("business rejection"), errors.New("transport failure")
	b, err := NewCircuitBreaker(BreakerConfig{Failures: 2, OpenTimeout: time.Hour, IsFailure: func(err error) bool { return errors.Is(err, failure) }})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []error{failure, ignored, failure, failure} {
		if err := b.Do(context.Background(), func(context.Context) error { return want }); !errors.Is(err, want) {
			t.Fatal(err)
		}
		expected := Closed
		if i == 3 {
			expected = Open
		}
		if b.State() != expected {
			t.Fatalf("step %d: %s", i, b.State())
		}
	}
	b.mu.Lock()
	b.until = time.Now().Add(-time.Second)
	b.mu.Unlock()
	if err := b.Do(context.Background(), func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if b.State() != Open {
		t.Fatal("failed probe did not reopen")
	}
}

func TestBreakerPanicCountsAsFailureAndPropagates(t *testing.T) {
	b, err := NewCircuitBreaker(BreakerConfig{Failures: 1})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if p := recover(); p != "boom" {
				t.Errorf("panic=%v", p)
			}
		}()
		_ = b.Do(context.Background(), func(context.Context) error { panic("boom") })
	}()
	if b.State() != Open {
		t.Fatal("panic did not open breaker")
	}
}
