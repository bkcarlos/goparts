package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the same shutdown/cancellation contract for both schedulers.
func TestSchedulersDrainAndSkipCanceledJobs(t *testing.T) {
	for _, kind := range []string{"pool", "stream"} {
		t.Run(kind, func(t *testing.T) {
			events := make(chan Event, 3)
			var submit func(context.Context, Task) error
			var closePool func()
			var wait func(context.Context) error
			if kind == "pool" {
				p, err := New(1, 2, func(e Event) { events <- e })
				if err != nil {
					t.Fatal(err)
				}
				submit, closePool, wait = p.Submit, p.Close, p.Wait
			} else {
				p, err := NewStream[string](1, 2, func(e Event) { events <- e })
				if err != nil {
					t.Fatal(err)
				}
				submit = func(ctx context.Context, fn Task) error { return p.Submit(ctx, "key", fn) }
				closePool, wait = p.Close, p.Wait
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release, entered := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer func() {
				unblock()
				closePool()
				if err := wait(ctx); err != nil {
					t.Error(err)
				}
			}()
			if err := submit(ctx, func(context.Context) error { close(entered); <-release; return nil }); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			canceled, stop := context.WithCancel(ctx)
			var executed atomic.Int32
			if err := submit(canceled, func(context.Context) error { executed.Add(1); return nil }); err != nil {
				t.Fatal(err)
			}
			stop()
			want := errors.New("task failed")
			if err := submit(ctx, func(context.Context) error { executed.Add(1); return want }); err != nil {
				t.Fatal(err)
			}
			closePool()
			closePool()
			if err := submit(ctx, func(context.Context) error { return nil }); !errors.Is(err, ErrClosed) {
				t.Fatalf("submit after close: %v", err)
			}
			canceledWait, stopWait := context.WithCancel(ctx)
			stopWait()
			if err := wait(canceledWait); !errors.Is(err, context.Canceled) {
				t.Fatalf("wait: %v", err)
			}
			unblock()
			if err := wait(ctx); err != nil {
				t.Fatal(err)
			}
			if executed.Load() != 1 {
				t.Fatalf("executed %d tasks", executed.Load())
			}
			for i, wantErr := range []error{nil, context.Canceled, want} {
				select {
				case e := <-events:
					if !errors.Is(e.Err, wantErr) || e.Wait < 0 || e.Duration < 0 {
						t.Fatalf("event %d: %+v", i, e)
					}
				default:
					t.Fatalf("missing event %d", i)
				}
			}
		})
	}
}

func TestPoolCloseReleasesBlockedSubmitters(t *testing.T) {
	p, err := New(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		close(release)
		p.Close()
		if err := p.Wait(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := p.Submit(ctx, func(context.Context) error { close(entered); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			results <- p.Submit(ctx, func(context.Context) error { t.Error("unexpected execution"); return nil })
		}()
	}
	p.Close()
	for i := 0; i < 32; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, ErrClosed) {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Fatal("blocked submitter leaked")
		}
	}
}
