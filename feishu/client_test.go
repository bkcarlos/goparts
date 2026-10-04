package feishu

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

func TestSignature(t *testing.T) {
	// Independent fixture generated with Python's hmac/sha256 implementation.
	if got := sign("1599360473", "secret"); got != "q4jswNiMy51J5JuQV566yJat0/lQ/c+22kINzUgKsGU=" {
		t.Fatalf("unexpected signature: %s", got)
	}
}

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSignedMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message Message
		key     string
	}{
		{"text", Text("你好"), "content"},
		{"image", Image("img_key"), "content"},
		{"post", Post(map[string]PostContent{"zh_cn": {Title: "通知", Content: [][]PostElement{{{Tag: "text", Text: "已发布"}}}}}), "content"},
		{"card", Markdown("通知", "**已发布**"), "card"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					t.Error("invalid request headers")
				}
				var got map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
					return
				}
				var ts, signature, msgType string
				for key, dst := range map[string]*string{"timestamp": &ts, "sign": &signature, "msg_type": &msgType} {
					if err := json.Unmarshal(got[key], dst); err != nil {
						t.Error(err)
					}
				}
				if ts == "" || signature != sign(ts, "secret") {
					t.Error("signature missing or invalid")
				}
				if msgType != tc.message.MsgType || len(got[tc.key]) == 0 {
					t.Error("message payload missing")
				}
				if tc.name == "text" && string(got["content"]) != `{"text":"你好"}` {
					t.Errorf("unexpected text: %s", got["content"])
				}
				if tc.name == "card" && got["content"] != nil {
					t.Error("card should be top-level")
				}
				io.WriteString(w, `{"code":0}`)
			}))
			defer srv.Close()
			if err := newTestClient(t, Config{WebhookURL: srv.URL, Secret: "secret"}).Send(context.Background(), tc.message); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResponseHandling(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
		apiCode   int
	}{
		{"success", 200, `{"code":0,"msg":"success"}`, false, 0},
		{"legacy", 200, `{"StatusCode":0}`, false, 0},
		{"api failure", 200, `{"code":19021,"msg":"signature failed"}`, true, 19021},
		{"legacy failure", 200, `{"StatusCode":1,"StatusMessage":"failed"}`, true, 1},
		{"mixed failure", 200, `{"code":1,"StatusCode":0}`, true, 1},
		{"rate limit", 429, `limited`, true, 0},
		{"server failure", 503, `unavailable`, true, 0},
		{"malformed", 200, `<html>`, true, 0},
		{"missing status", 200, `{}`, true, 0},
		{"null", 200, `null`, true, 0},
		{"empty", 204, ``, true, 0},
		{"too large", 200, strings.Repeat(" ", maxResponseBytes+1), true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			err := newTestClient(t, Config{WebhookURL: srv.URL}).SendText(context.Background(), "test")
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if tc.apiCode != 0 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != tc.apiCode {
					t.Fatalf("API error = %v", err)
				}
			}
			if tc.status >= 300 {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status {
					t.Fatalf("HTTP error = %v", err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected retry")
			}
		})
	}
}

func TestRedirectBlocked(t *testing.T) {
	var calls atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	custom := &http.Client{}
	err := newTestClient(t, Config{WebhookURL: src.URL, HTTPClient: custom}).SendText(context.Background(), "private")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 307 || calls.Load() != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
	if custom.CheckRedirect != nil {
		t.Fatal("caller client modified")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTimeoutCancellationAndURLPrivacy(t *testing.T) {
	hc := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	c := newTestClient(t, Config{WebhookURL: "https://example.com/secret-token", Timeout: 10 * time.Millisecond, HTTPClient: hc})
	err := c.SendText(context.Background(), "test")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SendText(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestValidationAndUnsignedConcurrentSend(t *testing.T) {
	for _, u := range []string{"", "/relative", "ftp://example.com", "https://user:pass@example.com", "https://example.com/#frag"} {
		if _, err := New(Config{WebhookURL: u}); err == nil {
			t.Errorf("accepted URL %q", u)
		}
	}
	if _, err := New(Config{WebhookURL: "https://example.com", Timeout: -1}); err == nil {
		t.Fatal("accepted negative timeout")
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if got["sign"] != nil || got["timestamp"] != nil {
			t.Error("unsigned payload contains signing fields")
		}
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()
	c := newTestClient(t, Config{WebhookURL: srv.URL})
	for _, msg := range []Message{Text(strings.Repeat("x", MaxMessageBytes)), {}, Card(make(chan int))} {
		if err := c.Send(context.Background(), msg); err == nil {
			t.Error("invalid message accepted")
		}
	}
	if err := c.Send(nil, Text("test")); err == nil {
		t.Error("nil context accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid messages sent")
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.SendText(context.Background(), "test"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 10 {
		t.Fatalf("got %d requests", calls.Load())
	}
}
