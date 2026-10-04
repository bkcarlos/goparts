package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type statusFailure int

func (e statusFailure) Error() string       { return fmt.Sprintf("status %d", e) }
func (e statusFailure) HTTPStatusCode() int { return int(e) }

type networkFailure struct{ timeout, temporary bool }

func (e networkFailure) Error() string   { return "network failure" }
func (e networkFailure) Timeout() bool   { return e.timeout }
func (e networkFailure) Temporary() bool { return e.temporary }

func TestRetryPresetsOnlyAllowSelectedFailures(t *testing.T) {
	for _, code := range []int{200, 400, 401, 403, 404, 408, 409, 429, 500, 501, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", statusFailure(code))
			want := code == 408 || code == 429 || code == 502 || code == 503 || code == 504
			if got := ReadOnlyHTTPConfig().RetryIf(err); got != want {
				t.Fatalf("HTTP status %d retry=%v", code, got)
			}
			if NetworkConfig().RetryIf(err) {
				t.Fatal("network preset accepted status error")
			}
		})
	}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{networkFailure{timeout: true}, true}, {networkFailure{temporary: true}, true}, {networkFailure{}, false}, {errors.New("permanent"), false}, {nil, false},
	} {
		for _, cfg := range []Config{NetworkConfig(), ReadOnlyHTTPConfig()} {
			if got := cfg.RetryIf(tc.err); got != tc.want {
				t.Errorf("%v retry=%v", tc.err, got)
			}
		}
	}
}

func TestAttemptObserverIncludesFinalSuccessAndError(t *testing.T) {
	want := errors.New("transient")
	var events []Attempt
	r := mustNew(t, Config{MaxAttempts: 3, InitialDelay: time.Nanosecond, RetryIf: func(error) bool { return true }, OnAttempt: func(a Attempt) { events = append(events, a) }})
	calls := 0
	if err := r.Do(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return want
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("attempt events=%d", len(events))
	}
	for i, e := range events {
		expected := want
		if i == 2 {
			expected = nil
		}
		if e.Number != i+1 || e.Duration < 0 || !errors.Is(e.Err, expected) {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
}

func TestConvenienceFunctionsPreserveErrorsAndCancellation(t *testing.T) {
	if err := Retry(nil, Config{}); err == nil {
		t.Fatal("nil callback accepted")
	}
	if err := Retry(func() error { t.Error("invalid config executed callback"); return nil }, Config{MaxAttempts: -1}); err == nil {
		t.Fatal("bad config accepted")
	}
	want := errors.New("permanent")
	if err := Retry(func() error { return want }, Config{}); !errors.Is(err, want) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RetryWithContext(ctx, func(context.Context) error { t.Error("canceled callback executed"); return nil }, Config{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
