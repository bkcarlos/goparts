package logger

import (
	"context"
	"io"
	"log/slog"
	"sync"
)

type Level = slog.Level

const (
	DebugLevel = slog.LevelDebug
	InfoLevel  = slog.LevelInfo
	WarnLevel  = slog.LevelWarn
	ErrorLevel = slog.LevelError
)

func ToLevel(text string) (Level, error) {
	var l slog.Level
	err := l.UnmarshalText([]byte(text))
	return l, err
}

var globalMu sync.Mutex
var globalCloser io.Closer

// InitGlobalLogger explicitly replaces slog.Default. An optional closer transfers
// ownership to this package; ordinary Config.Writer remains caller-owned.
func InitGlobalLogger(cfg Config, closer ...io.Closer) error {
	l, err := New(cfg)
	if err != nil {
		return err
	}
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalCloser != nil {
		if err := globalCloser.Close(); err != nil {
			return err
		}
	}
	globalCloser = nil
	if len(closer) > 0 {
		globalCloser = closer[0]
	}
	slog.SetDefault(l)
	return nil
}
func CloseGlobalLogger() error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalCloser == nil {
		return nil
	}
	err := globalCloser.Close()
	globalCloser = nil
	return err
}
func Debug(msg string, args ...any) { slog.Debug(msg, args...) }
func Info(msg string, args ...any)  { slog.Info(msg, args...) }
func Warn(msg string, args ...any)  { slog.Warn(msg, args...) }
func Error(msg string, args ...any) { slog.Error(msg, args...) }

// TraceWriter writes structured HTTP trace records as redacted JSON lines. Body
// capture is opt-in at the caller and should exclude application secrets.
type TraceWriter struct{ logger *slog.Logger }

func NewTraceWriter(w io.Writer) *TraceWriter {
	l := slog.New(RedactingHandler(slog.NewJSONHandler(w, nil)))
	return &TraceWriter{l}
}
func (w *TraceWriter) Record(ctx context.Context, fields ...any) {
	w.logger.InfoContext(ctx, "http_trace", fields...)
}
