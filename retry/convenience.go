package retry

import (
	"context"
	"errors"
	"net"
	"time"
)

func DefaultConfig() Config {
	return Config{MaxAttempts: 3, InitialDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second, Multiplier: 2}
}

// NetworkConfig retries only temporary/time-out errors; operations must be safe to repeat.
func NetworkConfig() Config {
	cfg := DefaultConfig()
	cfg.RetryIf = func(err error) bool { var n net.Error; return errors.As(err, &n) && (n.Timeout() || n.Temporary()) }
	return cfg
}

// ReadOnlyHTTPConfig also accepts structural status errors. Do not use this
// preset for non-idempotent writes without application-level idempotency keys.
func ReadOnlyHTTPConfig() Config {
	cfg := NetworkConfig()
	network := cfg.RetryIf
	cfg.RetryIf = func(err error) bool {
		var s interface{ HTTPStatusCode() int }
		if errors.As(err, &s) {
			code := s.HTTPStatusCode()
			return code == 408 || code == 429 || code == 502 || code == 503 || code == 504
		}
		return network(err)
	}
	return cfg
}
func Retry(operation func() error, cfg Config) error {
	if operation == nil {
		return errors.New("retry: operation required")
	}
	return RetryWithContext(context.Background(), func(context.Context) error { return operation() }, cfg)
}
func RetryWithContext(ctx context.Context, operation func(context.Context) error, cfg Config) error {
	r, err := New(cfg)
	if err != nil {
		return err
	}
	return r.Do(ctx, operation)
}
