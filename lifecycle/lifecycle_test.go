package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mustNew(t *testing.T, cfg Config) *Manager {
	t.Helper()
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskFailureCancelsPeersThenCleansUp(t *testing.T) {
	m := mustNew(t, Config{})
	want := errors.New("worker failed")
	peerStarted := make(chan struct{})
	var peerStopped atomic.Bool
	must(t, m.Add("peer", func(ctx context.Context) error {
		close(peerStarted)
		<-ctx.Done()
		peerStopped.Store(true)
		return ctx.Err()
	}))
	must(t, m.Add("worker", func(context.Context) error { <-peerStarted; return want }))
	var order []string
	for _, name := range []string{"database", "cache"} {
		name := name
		must(t, m.OnStop(name, func(ctx context.Context) error {
			if !peerStopped.Load() {
				t.Error("cleanup ran before tasks drained")
			}
			if ctx.Err() != nil {
				t.Error("cleanup received canceled context")
			}
			order = append(order, name)
			return nil
		}))
	}
	err := m.Run(context.Background())
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("error: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"cache", "database"}) {
		t.Fatalf("order: %v", order)
	}
}

func TestParentCancellationAndFreshCleanupContext(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "value"))
	m := mustNew(t, Config{})
	must(t, m.Add("wait", func(ctx context.Context) error { cancel(); <-ctx.Done(); return ctx.Err() }))
	called := false
	must(t, m.OnStop("cleanup", func(ctx context.Context) error {
		called = true
		if ctx.Err() != nil || ctx.Value(key{}) != "value" {
			t.Error("bad cleanup context")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing cleanup deadline")
		}
		return nil
	}))
	if err := m.Run(ctx); err != nil || !called {
		t.Fatalf("called=%v error=%v", called, err)
	}
}

func TestCleanupErrorsJoinedAndPanicRecovered(t *testing.T) {
	m := mustNew(t, Config{})
	first, second := errors.New("first"), errors.New("second")
	must(t, m.Add("panic-task", func(context.Context) error { panic("boom") }))
	must(t, m.OnStop("first", func(context.Context) error { return first }))
	must(t, m.OnStop("panic-hook", func(context.Context) error { panic("hook boom") }))
	must(t, m.OnStop("second", func(context.Context) error { return second }))
	err := m.Run(context.Background())
	if !errors.Is(err, first) || !errors.Is(err, second) || !strings.Contains(err.Error(), "panic-task panicked") || !strings.Contains(err.Error(), "panic-hook panicked") {
		t.Fatalf("joined error: %v", err)
	}
}

func TestTaskAndHookShutdownTimeout(t *testing.T) {
	for _, mode := range []string{"task", "hook"} {
		t.Run(mode, func(t *testing.T) {
			m := mustNew(t, Config{ShutdownTimeout: 10 * time.Millisecond})
			release := make(chan struct{})
			finished := make(chan struct{})
			defer func() { close(release); <-finished }()
			block := func(context.Context) error { defer close(finished); <-release; return nil }
			if mode == "task" {
				must(t, m.Add("blocked", block))
				must(t, m.Add("trigger", func(context.Context) error { return nil }))
			} else {
				must(t, m.Add("trigger", func(context.Context) error { return nil }))
				must(t, m.OnStop("blocked", block))
			}
			if err := m.Run(context.Background()); !errors.Is(err, ErrShutdownTimeout) {
				t.Fatalf("timeout: %v", err)
			}
		})
	}
}

func TestRegistrationAndRunOnce(t *testing.T) {
	if _, err := New(Config{ShutdownTimeout: -1}); err == nil {
		t.Fatal("negative timeout accepted")
	}
	m := mustNew(t, Config{})
	if m.Add("", func(context.Context) error { return nil }) == nil || m.OnStop("nil", nil) == nil {
		t.Fatal("invalid registration accepted")
	}
	must(t, m.Add("done", func(context.Context) error { return nil }))
	if m.OnStop("done", func(context.Context) error { return nil }) == nil {
		t.Fatal("duplicate name accepted")
	}
	if m.Run(nil) == nil {
		t.Fatal("nil context accepted")
	}
	if m.RunSignals(nil) == nil {
		t.Fatal("nil signal context accepted")
	}
	must(t, m.Run(context.Background()))
	if !errors.Is(m.Run(context.Background()), ErrAlreadyStarted) || !errors.Is(m.Add("late", func(context.Context) error { return nil }), ErrAlreadyStarted) {
		t.Fatal("manager reused")
	}
}

func TestAlreadyCanceledSkipsTasksAndRunsHooks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := mustNew(t, Config{})
	must(t, m.Add("skip", func(context.Context) error { t.Error("task ran"); return nil }))
	called := false
	must(t, m.OnStop("cleanup", func(context.Context) error { called = true; return nil }))
	if err := m.RunSignals(ctx); err != nil || !called {
		t.Fatalf("cleanup=%v error=%v", called, err)
	}
	empty := mustNew(t, Config{})
	must(t, empty.Run(ctx))
}

func TestCancellationDoesNotHideJoinedFailures(t *testing.T) {
	m := mustNew(t, Config{})
	want := errors.New("flush failed")
	must(t, m.Add("trigger", func(context.Context) error { return nil }))
	must(t, m.Add("worker", func(ctx context.Context) error { <-ctx.Done(); return errors.Join(ctx.Err(), want) }))
	if err := m.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("lost failure: %v", err)
	}
	m = mustNew(t, Config{})
	must(t, m.Add("deadline", func(context.Context) error { return context.DeadlineExceeded }))
	if err := m.Run(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost task deadline: %v", err)
	}
}
