// Package retry provides bounded, context-aware retries for explicitly selected errors.
package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

type Config struct {
	MaxAttempts  int              // includes the initial attempt; zero defaults to 3
	InitialDelay time.Duration    // zero defaults to 100ms
	MaxDelay     time.Duration    // zero defaults to 5s
	Multiplier   float64          // zero defaults to 2; must be >= 1
	Jitter       float64          // [0,1]; 0 disables jitter
	RetryIf      func(error) bool // nil means no retries
}

// Retrier is immutable and safe to reuse concurrently if RetryIf is also safe.
type Retrier struct{ cfg Config }

func New(cfg Config) (*Retrier, error) {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.InitialDelay == 0 {
		cfg.InitialDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 5 * time.Second
	}
	if cfg.Multiplier == 0 {
		cfg.Multiplier = 2
	}
	if cfg.MaxAttempts < 1 || cfg.InitialDelay < 0 || cfg.MaxDelay < cfg.InitialDelay || cfg.Multiplier < 1 || math.IsNaN(cfg.Multiplier) || math.IsInf(cfg.Multiplier, 0) || cfg.Jitter < 0 || cfg.Jitter > 1 || math.IsNaN(cfg.Jitter) {
		return nil, errors.New("retry: invalid attempts, delay, multiplier or jitter")
	}
	return &Retrier{cfg: cfg}, nil
}

// Do runs operation at most MaxAttempts times and returns the last operation
// error, or ctx.Err() when canceled. It never sleeps after the last attempt.
// Panics are not recovered. The caller is responsible for safe repeat execution.
func (r *Retrier) Do(ctx context.Context, operation func(context.Context) error) error {
	if ctx == nil || operation == nil {
		return errors.New("retry: context and operation are required")
	}
	delay := r.cfg.InitialDelay
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation(ctx)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if attempt >= r.cfg.MaxAttempts || r.cfg.RetryIf == nil || !r.cfg.RetryIf(err) {
			return err
		}
		timer := time.NewTimer(r.jitter(delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = capDuration(float64(delay)*r.cfg.Multiplier, r.cfg.MaxDelay)
	}
}

func (r *Retrier) jitter(delay time.Duration) time.Duration {
	if r.cfg.Jitter == 0 {
		return delay
	}
	factor := 1 - r.cfg.Jitter + 2*r.cfg.Jitter*rand.Float64()
	return capDuration(float64(delay)*factor, r.cfg.MaxDelay)
}

func capDuration(value float64, maximum time.Duration) time.Duration {
	if value >= float64(maximum) {
		return maximum
	}
	if value <= 0 {
		return 0
	}
	return time.Duration(value)
}
