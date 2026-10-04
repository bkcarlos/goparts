// Package ratelimit provides local token buckets and circuit breakers.
package ratelimit

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

type RateLimiter interface {
	Allow() bool
	Wait(context.Context) error
}
type Bucket struct {
	mu                     sync.Mutex
	rate, capacity, tokens float64
	last                   time.Time
}

func New(rate float64, burst int) (*Bucket, error) {
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) || burst < 1 {
		return nil, errors.New("ratelimit: invalid rate/burst")
	}
	return &Bucket{rate: rate, capacity: float64(burst), tokens: float64(burst), last: time.Now()}, nil
}
func (b *Bucket) take() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = math.Min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	seconds := (1 - b.tokens) / b.rate
	if seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return max(time.Nanosecond, time.Duration(seconds*float64(time.Second)))
}
func (b *Bucket) Allow() bool { return b.take() == 0 }
func (b *Bucket) Wait(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ratelimit: context required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		delay := b.take()
		if delay == 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type State string

const (
	Closed   State = "closed"
	Open     State = "open"
	HalfOpen State = "half_open"
)

var ErrOpen = errors.New("ratelimit: circuit open")

type BreakerConfig struct {
	Failures    int
	OpenTimeout time.Duration
	IsFailure   func(error) bool
}
type CircuitBreaker struct {
	mu         sync.Mutex
	cfg        BreakerConfig
	state      State
	failures   int
	until      time.Time
	generation uint64
	probe      bool
}

func NewCircuitBreaker(cfg BreakerConfig) (*CircuitBreaker, error) {
	if cfg.Failures == 0 {
		cfg.Failures = 5
	}
	if cfg.OpenTimeout == 0 {
		cfg.OpenTimeout = 30 * time.Second
	}
	if cfg.Failures < 1 || cfg.OpenTimeout < 0 {
		return nil, errors.New("ratelimit: invalid breaker config")
	}
	return &CircuitBreaker{cfg: cfg, state: Closed}, nil
}
func (b *CircuitBreaker) State() State { b.mu.Lock(); defer b.mu.Unlock(); b.advance(); return b.state }
func (b *CircuitBreaker) advance() {
	if b.state == Open && !time.Now().Before(b.until) {
		b.state = HalfOpen
		b.generation++
		b.probe = false
	}
}
func (b *CircuitBreaker) Do(ctx context.Context, fn func(context.Context) error) (err error) {
	if ctx == nil || fn == nil {
		return errors.New("ratelimit: context/operation required")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	b.advance()
	if b.state == Open || b.state == HalfOpen && b.probe {
		b.mu.Unlock()
		return ErrOpen
	}
	if b.state == HalfOpen {
		b.probe = true
	}
	generation := b.generation
	b.mu.Unlock()
	completed := false
	defer func() {
		failed := !completed || err != nil
		if completed && b.cfg.IsFailure != nil {
			failed = b.cfg.IsFailure(err)
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if generation != b.generation {
			return
		}
		if failed {
			b.failures++
			if b.state == HalfOpen || b.failures >= b.cfg.Failures {
				b.state = Open
				b.until = time.Now().Add(b.cfg.OpenTimeout)
				b.generation++
				b.probe = false
			}
		} else {
			b.failures = 0
			if b.state == HalfOpen {
				b.state = Closed
				b.generation++
				b.probe = false
			}
		}
	}()
	err = fn(ctx)
	completed = true
	return err
}
