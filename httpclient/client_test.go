package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustNew(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestJSONAndHeaderIsolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "original" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" || r.Method != http.MethodPost {
			t.Errorf("request: %s %v", r.Method, r.Header)
		}
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["name"] != "example" {
			t.Errorf("input=%v, err=%v", input, err)
		}
		w.Header().Set("X-Trace", "trace1")
		io.WriteString(w, `{"id":42}`)
	}))
	defer srv.Close()
	headers := http.Header{"authorization": []string{"original"}}
	c := mustNew(t, Config{Headers: headers})
	headers["authorization"][0] = "changed"
	var result struct {
		ID int `json:"id"`
	}
	resp, err := c.DoJSON(context.Background(), http.MethodPost, srv.URL, map[string]string{"name": "example"}, &result)
	if err != nil || result.ID != 42 || resp.Headers.Get("X-Trace") != "trace1" {
		t.Fatalf("result=%+v, response=%v, err=%v", result, resp, err)
	}
}

func TestResponsesAndNoRetries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		limit     int64
		wantError bool
	}{
		{"success", 200, `{"ok":true}`, 100, false},
		{"empty", 204, "", 100, false},
		{"exact limit", 200, `{}`, 2, false},
		{"too large", 200, `{}`, 1, true},
		{"malformed", 200, `oops`, 100, true},
		{"trailing", 200, `{} {}`, 100, true},
		{"failure", 503, `{"error":"down"}`, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c := mustNew(t, Config{MaxResponseBytes: tc.limit})
			var result map[string]any
			resp, err := c.DoJSON(context.Background(), http.MethodGet, srv.URL, nil, &result)
			if (err != nil) != tc.wantError || resp == nil || calls.Load() != 1 {
				t.Fatalf("response=%v, err=%v, calls=%d", resp, err, calls.Load())
			}
			if tc.name == "too large" && (!errors.Is(err, ErrResponseTooLarge) || resp.Body != nil) {
				t.Fatalf("size error: %v", err)
			}
			if tc.status >= 300 {
				var status *StatusError
				if !errors.As(err, &status) || status.StatusCode != tc.status || string(resp.Body) != tc.body {
					t.Fatalf("status error: %v", err)
				}
			}
		})
	}
}

func TestRedirectAndRequestHeaders(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Value") != "request" {
			t.Errorf("header override: %v", r.Header)
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c := mustNew(t, Config{Headers: http.Header{"X-Value": []string{"default"}}})
	_, err := c.Do(context.Background(), Request{URL: srv.URL, Headers: http.Header{"x-value": []string{"request"}}})
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != 307 || forwarded.Load() != 0 {
		t.Fatalf("redirect: %v", err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeadlineCancellationAndURLPrivacy(t *testing.T) {
	c := mustNew(t, Config{Timeout: 10 * time.Millisecond, Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })})
	_, err := c.Do(context.Background(), Request{URL: "https://example.com/secret-token"})
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Do(ctx, Request{URL: "https://example.com"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

type trackedBody struct {
	err    error
	closed bool
}

func (b *trackedBody) Read([]byte) (int, error) { return 0, b.err }
func (b *trackedBody) Close() error             { b.closed = true; return nil }

func TestReadFailureClosesBody(t *testing.T) {
	want := errors.New("read failed")
	body := &trackedBody{err: want}
	c := mustNew(t, Config{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})})
	resp, err := c.Do(context.Background(), Request{URL: "http://example.com"})
	if !errors.Is(err, want) || !body.closed || resp.Body != nil {
		t.Fatalf("body=%+v err=%v", body, err)
	}
}

func TestValidationAndConcurrentRequests(t *testing.T) {
	for _, cfg := range []Config{{Timeout: -1}, {MaxResponseBytes: -1}, {MaxResponseBytes: int64(^uint64(0) >> 1)}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	c := mustNew(t, Config{})
	for _, output := range []any{42, (*int)(nil)} {
		if _, err := c.DoJSON(context.Background(), "POST", "https://example.com", nil, output); err == nil {
			t.Fatal("invalid JSON output accepted")
		}
	}
	for _, endpoint := range []string{"", "/relative", "ftp://example.com", "https://user:pass@example.com", "://"} {
		if _, err := c.Do(context.Background(), Request{URL: endpoint}); err == nil {
			t.Fatalf("accepted %q", endpoint)
		}
	}
	if _, err := c.Do(nil, Request{URL: "https://example.com"}); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := c.Do(context.Background(), Request{URL: "https://example.com", Method: "bad method"}); err == nil {
		t.Fatal("bad method accepted")
	}
	if _, err := c.DoJSON(context.Background(), "POST", "https://example.com", make(chan int), nil); err == nil {
		t.Fatal("unsupported JSON accepted")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `ok`) }))
	defer srv.Close()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Do(context.Background(), Request{URL: srv.URL})
			if err != nil || string(resp.Body) != "ok" {
				t.Errorf("response=%v err=%v", resp, err)
			}
		}()
	}
	wg.Wait()
}
