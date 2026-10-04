package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, id    string
		trust, keep bool
	}{
		{"trusted", "trace_123.a-b", true, true},
		{"untrusted", "trace_123", false, false},
		{"invalid characters", "bad id", true, false},
		{"too long", strings.Repeat("a", 129), true, false},
		{"missing", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := RequestID(tc.trust)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ID(r.Context()) }))
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Request-Id", tc.id)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if got == "" || got != w.Header().Get("X-Request-Id") || !validID.MatchString(got) {
				t.Fatalf("invalid propagated ID %q", got)
			}
			if (got == tc.id) != tc.keep {
				t.Fatalf("incoming ID trust violated: %q", got)
			}
		})
	}
}

func TestAuthRejectsMalformedHeaderWithoutCallingHandler(t *testing.T) {
	for _, header := range []string{"", "Basic token", "Bearer", "Bearer token extra", "Bearer wrong"} {
		t.Run(header, func(t *testing.T) {
			h := AuthBearer(StaticToken("token"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unauthorized handler ran") }))
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("status=%d headers=%v", w.Code, w.Header())
			}
		})
	}
}

func TestCORSPreflightAndCredentialBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cfg             CORSConfig
		origin, headers string
		want            int
		next            bool
	}{
		{"case insensitive headers", CORSConfig{Origins: []string{"https://client.test"}, Methods: []string{"POST"}, Headers: []string{"Content-Type", "X-Trace"}, Credentials: true, MaxAge: time.Minute}, "https://client.test", "content-type, x-trace", 204, false},
		{"forbidden header", CORSConfig{Origins: []string{"https://client.test"}, Methods: []string{"POST"}, Headers: []string{"Content-Type"}}, "https://client.test", "X-Admin", 403, false},
		{"forbidden origin", CORSConfig{Origins: []string{"https://client.test"}, Methods: []string{"POST"}}, "https://evil.test", "", 403, false},
		{"wildcard credentials", CORSConfig{Origins: []string{"*"}, Methods: []string{"POST"}, Credentials: true}, "https://client.test", "", 403, false},
		{"wildcard anonymous", CORSConfig{Origins: []string{"*"}, Methods: []string{"POST"}}, "https://client.test", "", 204, false},
		{"no origin", CORSConfig{}, "", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := CORS(tc.cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			req := httptest.NewRequest("OPTIONS", "/", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", tc.headers)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want || called != tc.next {
				t.Fatalf("status=%d next=%v", w.Code, called)
			}
			if tc.want == 204 {
				if w.Header().Get("Access-Control-Allow-Origin") != tc.origin {
					t.Fatal("allowed origin missing")
				}
				if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Access-Control-Request-Headers") {
					t.Fatal("preflight cache varies incorrectly")
				}
				if tc.cfg.Credentials && w.Header().Get("Access-Control-Allow-Credentials") != "true" {
					t.Fatal("credentials missing")
				}
				if tc.cfg.MaxAge > 0 && w.Header().Get("Access-Control-Max-Age") != "60" {
					t.Fatal("wrong max age")
				}
			}
		})
	}
}

func TestAccessLogFinalStatusBytesAndPrivacy(t *testing.T) {
	var event AccessEvent
	h := AccessLog(nil, func(e AccessEvent) { event = e })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.WriteHeader(500)
		io.WriteString(w, "你好")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/resource?token=private", nil))
	if w.Code != 201 || event.Status != 201 || event.Bytes != 6 || event.Path != "/resource" || event.Method != "POST" {
		t.Fatalf("wrong access event: %+v", event)
	}
}

func TestRecoverPreservesAbortHandler(t *testing.T) {
	h := Recover(func(context.Context, any) { t.Error("abort reported as application panic") })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Errorf("abort panic swallowed: %v", got)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestBodyLimitKnownLengthRejectsBeforeHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int64
		body   string
		want   int
		called bool
	}{
		{"over", 2, "abc", 413, false}, {"exact", 3, "abc", 200, true}, {"empty", 0, "", 200, true}, {"bad config", -1, "", 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := BodyLimit(tc.limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				b, err := io.ReadAll(r.Body)
				if err != nil || string(b) != tc.body {
					t.Errorf("body=%q err=%v", b, err)
				}
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
			if w.Code != tc.want || called != tc.called {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
		})
	}
}
