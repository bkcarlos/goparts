package httpclient

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value    string
		duration time.Duration
		ok       bool
	}{
		{" 12 ", 12 * time.Second, true}, {"0", 0, true}, {"-1", 0, false}, {"+1", 0, false}, {"1.5", 0, false}, {"", 0, false},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true}, {now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"999999999999999999999999", time.Duration(math.MaxInt64), true}, {"tomorrow", 0, false},
	} {
		d, ok := ParseRetryAfter(tc.value, now)
		if d != tc.duration || ok != tc.ok {
			t.Errorf("%q = %v %v", tc.value, d, ok)
		}
	}
}

func TestStatusMetadataSurvivesBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(429)
		io.WriteString(w, "too much data")
	}))
	defer srv.Close()
	c, _ := New(Config{MaxResponseBytes: 3})
	response, err := c.Do(context.Background(), Request{URL: srv.URL})
	var status *StatusError
	if !errors.As(err, &status) || !errors.Is(err, ErrResponseTooLarge) || status.Method != "GET" {
		t.Fatalf("error=%v", err)
	}
	if wait, ok := status.RetryDelay(time.Now()); !ok || wait != 12*time.Second {
		t.Fatal("Retry-After lost")
	}
	response.Headers.Set("Retry-After", "0")
	if status.Headers.Get("Retry-After") != "12" {
		t.Fatal("error headers alias response")
	}
}
