package retry

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func mustNew(t *testing.T, cfg Config) *Retrier {
	t.Helper()
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAttemptsAndPredicate(t *testing.T) {
	transient := errors.New("transient")
	permanent := errors.New("permanent")
	r := mustNew(t, Config{MaxAttempts: 3, InitialDelay: time.Nanosecond, RetryIf: func(err error) bool { return errors.Is(err, transient) }})
	calls := 0
	if err := r.Do(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return transient
		}
		return nil
	}); err != nil || calls != 3 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
	calls = 0
	if err := r.Do(context.Background(), func(context.Context) error { calls++; return transient }); err != transient || calls != 3 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
	calls = 0
	if err := r.Do(context.Background(), func(context.Context) error { calls++; return permanent }); err != permanent || calls != 1 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
	calls = 0
	if err := mustNew(t, Config{}).Do(context.Background(), func(context.Context) error { calls++; return transient }); err != transient || calls != 1 {
		t.Fatal("nil predicate retried")
	}
}

func TestCancellationDuringWaitAndBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := mustNew(t, Config{InitialDelay: time.Hour, MaxDelay: time.Hour, RetryIf: func(error) bool { cancel(); return true }})
	calls := 0
	if err := r.Do(ctx, func(context.Context) error { calls++; return errors.New("retry") }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if err := r.Do(ctx, func(context.Context) error { t.Fatal("operation ran on canceled context"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	r = mustNew(t, Config{InitialDelay: time.Hour, MaxDelay: time.Hour, RetryIf: func(error) bool { return true }})
	if err := r.Do(ctx, func(context.Context) error { return errors.New("retry") }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestBackoffBoundsAndOverflow(t *testing.T) {
	r := mustNew(t, Config{InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second, Multiplier: 3, Jitter: 0.2})
	d := r.cfg.InitialDelay
	for _, expected := range []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 900 * time.Millisecond, time.Second, time.Second} {
		if d != expected {
			t.Fatalf("delay=%v, want=%v", d, expected)
		}
		for i := 0; i < 100; i++ {
			j := r.jitter(d)
			if j < time.Duration(float64(d)*0.8) || j > time.Second || j > time.Duration(float64(d)*1.2) {
				t.Fatalf("jitter out of bounds: %v", j)
			}
		}
		d = capDuration(float64(d)*r.cfg.Multiplier, r.cfg.MaxDelay)
	}
	if capDuration(math.MaxFloat64, time.Duration(math.MaxInt64)) != time.Duration(math.MaxInt64) {
		t.Fatal("duration overflow")
	}
}

func TestLastAttemptDoesNotWait(t *testing.T) {
	r := mustNew(t, Config{MaxAttempts: 1, InitialDelay: time.Hour, MaxDelay: time.Hour, RetryIf: func(error) bool { t.Fatal("predicate on final attempt"); return true }})
	want := errors.New("failed")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Do(ctx, func(context.Context) error { return want }); err != want {
		t.Fatal(err)
	}
}

func TestConcurrentReuseAndValidation(t *testing.T) {
	r := mustNew(t, Config{InitialDelay: time.Nanosecond, RetryIf: func(error) bool { return true }})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count := 0
			if err := r.Do(context.Background(), func(context.Context) error {
				count++
				if count == 1 {
					return errors.New("retry")
				}
				return nil
			}); err != nil || count != 2 {
				t.Errorf("calls=%d err=%v", count, err)
			}
		}()
	}
	wg.Wait()
	for _, cfg := range []Config{{MaxAttempts: -1}, {InitialDelay: -1}, {MaxDelay: -1}, {InitialDelay: time.Second, MaxDelay: time.Millisecond}, {Multiplier: 0.5}, {Multiplier: math.NaN()}, {Multiplier: math.Inf(1)}, {Jitter: -1}, {Jitter: 2}, {Jitter: math.NaN()}} {
		if _, err := New(cfg); err == nil {
			t.Errorf("invalid config accepted: %+v", cfg)
		}
	}
	if err := r.Do(nil, func(context.Context) error { return nil }); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := r.Do(context.Background(), nil); err == nil {
		t.Fatal("nil operation accepted")
	}
}

func TestRetryAfterMinimumAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	calls, delays := 0, 0
	r := mustNew(t, Config{InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond, Jitter: 1, RetryIf: func(error) bool { return true }, RetryAfter: func(error) (time.Duration, bool) { delays++; return time.Hour, true }})
	err := r.Do(ctx, func(context.Context) error { calls++; return errors.New("rate limited") })
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || delays != 1 {
		t.Fatalf("calls=%d delays=%d err=%v", calls, delays, err)
	}
	for _, wait := range []time.Duration{-1, 0} {
		r := mustNew(t, Config{InitialDelay: time.Nanosecond, RetryIf: func(error) bool { return true }, RetryAfter: func(error) (time.Duration, bool) { return wait, true }})
		attempts := 0
		if err := r.Do(context.Background(), func(context.Context) error {
			attempts++
			if attempts == 2 {
				return nil
			}
			return errors.New("retry")
		}); err != nil || attempts != 2 {
			t.Fatal(err)
		}
	}
	r = mustNew(t, Config{RetryAfter: func(error) (time.Duration, bool) { t.Fatal("delay called without retry permission"); return 0, true }})
	r.Do(context.Background(), func(context.Context) error { return errors.New("do not repeat") })
}
