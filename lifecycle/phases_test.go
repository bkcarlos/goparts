package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPhasesAndBudgets(t *testing.T) {
	m, _ := New(Config{ShutdownTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var order []string
	add := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	started := make(chan struct{})
	release := make(chan struct{})
	m.Add("task", func(ctx context.Context) error { close(started); <-ctx.Done(); add("drain"); return nil })
	m.OnQuiesce("intake", func(context.Context) error { add("quiesce"); return nil })
	m.OnStop("cleanup", func(context.Context) error { add("cleanup"); return nil })
	m.OnStop("slow", func(context.Context) error { <-release; return nil }, WithStopTimeout(time.Millisecond))
	go func() { <-started; cancel() }()
	err := m.Run(ctx)
	close(release)
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "quiesce" || order[1] != "drain" || order[2] != "cleanup" {
		t.Fatal(order)
	}
}
