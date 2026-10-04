package httpclient

import (
	"context"
	"net/http"
	"time"
)

// Hooks observe snapshots without consuming bodies. Headers use an allowlist;
// queries, credentials and raw errors are never included. Hooks run synchronously.
type Hooks struct {
	OnRequest  func(context.Context, RequestEvent)
	OnResponse func(context.Context, ResponseEvent)
}
type RequestEvent struct {
	Method, Host, Path string
	Headers            http.Header
}
type ResponseEvent struct {
	Method     string
	StatusCode int
	Duration   time.Duration
	Bytes      int64
	Failed     bool
	Headers    http.Header
}

func safeHeaders(h http.Header) http.Header {
	out := http.Header{}
	for _, k := range []string{"Content-Type", "Content-Length", "Accept", "Retry-After", "X-Request-Id"} {
		if v := h.Values(k); len(v) > 0 {
			out[k] = append([]string(nil), v...)
		}
	}
	return out
}
func (e *StatusError) HTTPStatusCode() int { return e.StatusCode }
