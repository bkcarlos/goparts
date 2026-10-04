// Package logger provides structured logging using the standard slog API.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Config configures a logger. Its zero value writes JSON at INFO to stdout.
// The caller owns Writer and is responsible for closing or flushing it.
type Config struct {
	Format      string       // "json" (default) or "text"
	Level       slog.Leveler // nil means INFO; use *slog.LevelVar for runtime changes
	Writer      io.Writer
	AddSource   bool
	Service     string
	Environment string
}

// New creates an independent logger without changing slog.Default().
func New(cfg Config) (*slog.Logger, error) {
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{Level: cfg.Level, AddSource: cfg.AddSource}
	var h slog.Handler
	switch cfg.Format {
	case "", "json":
		h = slog.NewJSONHandler(w, opts)
	case "text":
		h = slog.NewTextHandler(w, opts)
	default:
		return nil, fmt.Errorf("logger: unsupported format %q", cfg.Format)
	}
	l := slog.New(h)
	if cfg.Service != "" {
		l = l.With("service", cfg.Service)
	}
	if cfg.Environment != "" {
		l = l.With("environment", cfg.Environment)
	}
	return l, nil
}

type contextKey struct{}

// WithContext attaches a logger to a non-nil context. A nil logger uses slog.Default().
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	if l == nil {
		l = slog.Default()
	}
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext returns the attached logger, falling back to slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// WithFields derives a context with structured fields (for example request_id).
// The parent context's logger is unchanged. Arguments follow slog.Logger.With.
func WithFields(ctx context.Context, args ...any) context.Context {
	return WithContext(ctx, FromContext(ctx).With(args...))
}
