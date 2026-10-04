// Package middleware supplies composable standard net/http handlers.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, m ...Middleware) http.Handler {
	for i := len(m) - 1; i >= 0; i-- {
		h = m[i](h)
	}
	return h
}

type requestIDKey struct{}

func ID(ctx context.Context) string { s, _ := ctx.Value(requestIDKey{}).(string); return s }

var validID = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

// RequestID ignores untrusted incoming IDs unless trustIncoming is explicitly true.
func RequestID(trustIncoming bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !trustIncoming || !validID.MatchString(id) {
				var b [16]byte
				if _, err := rand.Read(b[:]); err != nil {
					http.Error(w, "request ID unavailable", 500)
					return
				}
				id = hex.EncodeToString(b[:])
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		})
	}
}

type AccessEvent struct {
	Method, Path, RequestID string
	Status                  int
	Bytes                   int64
	Duration                time.Duration
}
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *recorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// ResponseController can reach Flush/Hijack through Unwrap. AccessLog never logs
// queries, headers or bodies. observer and logger must be concurrent-safe.
func AccessLog(logger *slog.Logger, observer func(AccessEvent)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &recorder{ResponseWriter: w}
			defer func() {
				status := rw.status
				if status == 0 {
					status = 200
				}
				e := AccessEvent{r.Method, r.URL.Path, ID(r.Context()), status, rw.bytes, time.Since(start)}
				if observer != nil {
					observer(e)
				}
				if logger != nil {
					logger.InfoContext(r.Context(), "http_request", "method", e.Method, "path", e.Path, "request_id", e.RequestID, "status", e.Status, "bytes", e.Bytes, "duration", e.Duration)
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}
func Recover(onPanic func(context.Context, any)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					if onPanic != nil {
						onPanic(r.Context(), p)
					}
					http.Error(w, "internal server error", 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout uses net/http's buffered TimeoutHandler; it is not for SSE/WebSockets.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler { return http.TimeoutHandler(next, d, "request timed out") }
}
func BodyLimit(bytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bytes < 0 {
				http.Error(w, "invalid body limit", 500)
				return
			}
			if r.ContentLength > bytes {
				http.Error(w, "request too large", 413)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, bytes)
			next.ServeHTTP(w, r)
		})
	}
}

// AuthBearer accepts a validator so keys may rotate without rebuilding handlers.
func AuthBearer(validate func(context.Context, string) bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || validate == nil || !validate(r.Context(), parts[1]) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", 401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
func StaticToken(expected string) func(context.Context, string) bool {
	return func(_ context.Context, actual string) bool {
		return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
	}
}

type CORSConfig struct {
	Origins, Methods, Headers []string
	Credentials               bool
	MaxAge                    time.Duration
}

func CORS(cfg CORSConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Vary", "Origin")
			allowed := false
			for _, v := range cfg.Origins {
				if v == origin || v == "*" && !cfg.Credentials {
					allowed = true
				}
			}
			if !allowed {
				http.Error(w, "origin forbidden", 403)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if cfg.Credentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				method := r.Header.Get("Access-Control-Request-Method")
				if !contains(cfg.Methods, method) {
					http.Error(w, "method forbidden", 403)
					return
				}
				for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
					if h = strings.TrimSpace(h); h != "" && !containsFold(cfg.Headers, h) {
						http.Error(w, "header forbidden", 403)
						return
					}
				}
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.FormatInt(int64(cfg.MaxAge/time.Second), 10))
				}
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.Methods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.Headers, ", "))
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
