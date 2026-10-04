package attachment

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConfigAndTokenProvider(t *testing.T) {
	provider := func(context.Context) (string, error) { return "token", nil }
	for _, cfg := range []Config{
		{}, {TokenProvider: provider, Timeout: -1}, {TokenProvider: provider, MaxFileBytes: -1},
		{TokenProvider: provider, MaxResponseBytes: -1}, {TokenProvider: provider, MaxResponseBytes: math.MaxInt64},
		{TokenProvider: provider, BaseURL: "ftp://example.com"}, {TokenProvider: provider, BaseURL: "https://u:p@example.com"},
		{TokenProvider: provider, BaseURL: "https://example.com?token=secret"},
	} {
		if _, err := New(cfg); err == nil {
			t.Error("accepted invalid config")
		}
	}
	c, err := New(Config{TokenProvider: provider})
	if err != nil || c.timeout != DefaultTimeout || c.maxFileBytes != DefaultMaxFileBytes || c.maxResponseBytes != DefaultMaxResponseBytes {
		t.Fatalf("defaults: %v %v", c, err)
	}
	failed := errors.New("authentication failed")
	for _, tc := range []struct {
		name     string
		provider func(context.Context) (string, error)
		want     error
	}{
		{"failure", func(context.Context) (string, error) { return "", failed }, failed},
		{"empty", func(context.Context) (string, error) { return "", nil }, nil},
		{"timeout", func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(Config{TokenProvider: tc.provider, Timeout: 10 * time.Millisecond, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Error("must not call transport"); return nil, failed })}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"})
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestTokenIsAcquiredEachCallAndPrematureSuccessRejected(t *testing.T) {
	count := 0
	consume := true
	c, err := New(Config{
		TokenProvider: func(context.Context) (string, error) { count++; return strings.Repeat("x", count), nil },
		HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", count) {
				t.Error("stale token")
			}
			if consume {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					return nil, err
				}
			}
			r.Body.Close()
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"file_key":"file1"}}`))}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	if count != 2 {
		t.Fatal("provider was cached")
	}
	consume = false
	if _, err := c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"}); err == nil {
		t.Fatal("accepted success without consuming file")
	}
}
