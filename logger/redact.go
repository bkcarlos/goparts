package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

var bearerPattern = regexp.MustCompile(`(?i)Bearer\s+[^\s,;"']+`)
var sensitiveKeys = map[string]bool{"authorization": true, "proxy_authorization": true, "token": true, "access_token": true, "refresh_token": true, "api_key": true, "apikey": true, "password": true, "secret": true, "app_secret": true, "cookie": true, "set_cookie": true}

// RedactValue masks credential fields recursively and Bearer values in strings.
// Unserializable structured values are omitted rather than formatted unsafely.
func RedactValue(key string, value any) any {
	normalized := strings.ReplaceAll(strings.ToLower(key), "-", "_")
	if sensitiveKeys[normalized] || strings.HasSuffix(normalized, "_token") || strings.HasSuffix(normalized, "_secret") {
		return "[REDACTED]"
	}
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return bearerPattern.ReplaceAllString(v, "Bearer [REDACTED]")
	case error:
		return bearerPattern.ReplaceAllString(v.Error(), "Bearer [REDACTED]")
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, x := range v {
			result[k] = RedactValue(k, x)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, x := range v {
			result[i] = RedactValue("", x)
		}
		return result
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "[UNSERIALIZABLE]"
		}
		var decoded any
		if json.Unmarshal(b, &decoded) != nil {
			return "[UNSERIALIZABLE]"
		}
		switch decoded.(type) {
		case map[string]any, []any, string:
			return RedactValue(key, decoded)
		default:
			return decoded
		}
	}
}
func redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		children := a.Value.Group()
		args := make([]any, len(children))
		for i, child := range children {
			args[i] = redactAttr(child)
		}
		if sensitiveKeys[strings.ReplaceAll(strings.ToLower(a.Key), "-", "_")] {
			return slog.String(a.Key, "[REDACTED]")
		}
		return slog.Group(a.Key, args...)
	}
	return slog.Any(a.Key, RedactValue(a.Key, a.Value.Any()))
}

type redactingHandler struct{ next slog.Handler }

// RedactingHandler also sanitizes messages and attributes supplied by With.
func RedactingHandler(next slog.Handler) slog.Handler { return &redactingHandler{next} }
func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}
func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, bearerPattern.ReplaceAllString(r.Message, "Bearer [REDACTED]"), r.PC)
	r.Attrs(func(a slog.Attr) bool { out.AddAttrs(redactAttr(a)); return true })
	return h.next.Handle(ctx, out)
}
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = redactAttr(a)
	}
	return &redactingHandler{h.next.WithAttrs(out)}
}
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{h.next.WithGroup(name)}
}
