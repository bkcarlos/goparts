package workerpool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBackpressureShutdownPanic(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var mu sync.Mutex
	var errs []error
	p, _ := New(1, 0, func(e Event) { mu.Lock(); errs = append(errs, e.Err); mu.Unlock() })
	p.Submit(context.Background(), func(context.Context) error { close(entered); <-release; panic("private") })
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if !errors.Is(p.Submit(ctx, func(context.Context) error { return nil }), context.DeadlineExceeded) {
		t.Fatal("no backpressure")
	}
	p.Close()
	close(release)
	if err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 1 || !errors.Is(errs[0], ErrPanic) {
		t.Fatal(errs)
	}
	if !errors.Is(p.Submit(context.Background(), func(context.Context) error { return nil }), ErrClosed) {
		t.Fatal("closed")
	}
}
func TestKeyedOrderAndConcurrency(t *testing.T) {
	s, _ := NewStream[string](2, 20)
	release := make(chan struct{})
	entered := make(chan struct{})
	other := make(chan struct{})
	var got []int
	var mu sync.Mutex
	s.Submit(context.Background(), "a", func(context.Context) error { close(entered); <-release; return nil })
	<-entered
	for i := 0; i < 10; i++ {
		i := i
		s.Submit(context.Background(), "a", func(context.Context) error { mu.Lock(); got = append(got, i); mu.Unlock(); return nil })
	}
	s.Submit(context.Background(), "b", func(context.Context) error { close(other); return nil })
	select {
	case <-other:
	case <-time.After(time.Second):
		t.Fatal("different key blocked")
	}
	close(release)
	s.Close()
	s.Wait(context.Background())
	for i, n := range got {
		if i != n {
			t.Fatal(got)
		}
	}
	if len(got) != 10 {
		t.Fatal(got)
	}
}
