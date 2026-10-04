package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type AccumulatorConfig struct {
	Characters int
	Interval   time.Duration
	MaxBytes   int
	Flush      func(context.Context, string) error
}
type Accumulator struct {
	mu         sync.Mutex
	ctx        context.Context
	cfg        AccumulatorConfig
	text       strings.Builder
	pending    int
	err        error
	closed     bool
	stop, done chan struct{}
	once       sync.Once
}

// NewAccumulator flushes the full accumulated text. Callbacks run serially and
// must not reenter this accumulator. Close flushes pending text and stops timers.
func NewAccumulator(ctx context.Context, cfg AccumulatorConfig) (*Accumulator, error) {
	if ctx == nil || cfg.Flush == nil || cfg.Characters < 0 || cfg.Interval < 0 || cfg.MaxBytes < 0 {
		return nil, errors.New("chat: invalid accumulator config")
	}
	if cfg.Characters == 0 {
		cfg.Characters = 128
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 16 << 20
	}
	a := &Accumulator{ctx: ctx, cfg: cfg, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(a.done)
		var tick <-chan time.Time
		if cfg.Interval > 0 {
			timer := time.NewTicker(cfg.Interval)
			defer timer.Stop()
			tick = timer.C
		}
		for {
			select {
			case <-a.stop:
				return
			case <-ctx.Done():
				a.mu.Lock()
				a.err = ctx.Err()
				a.mu.Unlock()
				return
			case <-tick:
				a.Flush()
			}
		}
	}()
	return a, nil
}
func (a *Accumulator) Add(delta string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("chat: accumulator closed")
	}
	if a.err != nil {
		return a.err
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if len(delta) > a.cfg.MaxBytes-a.text.Len() {
		return errors.New("chat: accumulated text exceeds limit")
	}
	a.text.WriteString(delta)
	a.pending += utf8.RuneCountInString(delta)
	if a.pending >= a.cfg.Characters {
		return a.flush()
	}
	return nil
}
func (a *Accumulator) flush() error {
	if a.err != nil {
		return a.err
	}
	if a.pending == 0 {
		return nil
	}
	if err := a.ctx.Err(); err != nil {
		a.err = err
		return err
	}
	a.err = a.cfg.Flush(a.ctx, a.text.String())
	if a.err == nil {
		a.pending = 0
	}
	return a.err
}
func (a *Accumulator) Flush() error { a.mu.Lock(); defer a.mu.Unlock(); return a.flush() }
func (a *Accumulator) Text() string { a.mu.Lock(); defer a.mu.Unlock(); return a.text.String() }
func (a *Accumulator) Close() error {
	a.once.Do(func() { close(a.stop) })
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return a.flush()
}
