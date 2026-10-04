package apperror_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bkcarlos/goparts/apperror"
)

type nilSink struct{}

func (*nilSink) Report(context.Context, apperror.Record) error { panic("nil sink called") }

func TestReporterValidationAndNil(t *testing.T) {
	for _, sink := range []apperror.Sink{nil, apperror.SinkFunc(nil), (*nilSink)(nil)} {
		if _, err := apperror.NewReporter(apperror.ReporterConfig{Sink: sink}); err == nil {
			t.Fatal("accepted nil sink")
		}
	}
	var called bool
	reporter, err := apperror.NewReporter(apperror.ReporterConfig{Sink: apperror.SinkFunc(func(context.Context, apperror.Record) error { called = true; return nil })})
	if err != nil {
		t.Fatal(err)
	}
	if reporter.Capture(nil, nil) != nil || called {
		t.Fatal("nil error was reported")
	}
	if reporter.Capture(nil, errOrder) == nil || called {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(reporter.Capture(ctx, errOrder), context.Canceled) || called {
		t.Fatal("canceled report called sink")
	}
}

func TestReporterSnapshotDetailsAndFailures(t *testing.T) {
	for _, details := range []bool{false, true} {
		reportFailure := errors.New("sink unavailable")
		calls := 0
		sink := apperror.SinkFunc(func(ctx context.Context, r apperror.Record) error {
			calls++
			if r.Code != "order.not_found" || r.Message != "订单不存在" || r.Fields["request_id"] != "req-1" || r.Fields["order_id"] != "42" {
				t.Fatalf("report=%+v", r)
			}
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private-detail") != details {
				t.Fatalf("detail policy failed: %s", data)
			}
			r.Fields["order_id"] = "changed by sink"
			return reportFailure
		})
		reporter, err := apperror.NewReporter(apperror.ReporterConfig{Sink: sink, IncludeDetail: details})
		if err != nil {
			t.Fatal(err)
		}
		original := errOrder.Wrap(errors.New("private-detail"), apperror.WithFields(apperror.Fields{"order_id": "old"}))
		contextFields := apperror.Fields{"order_id": "42", "request_id": "req-1"}
		err = reporter.Capture(context.Background(), original, contextFields)
		if !errors.Is(err, reportFailure) || calls != 1 {
			t.Fatalf("report failure not propagated or retried: %v", err)
		}
		if contextFields["order_id"] != "42" || apperror.Describe(original).Fields["order_id"] != "old" {
			t.Fatal("sink mutated original fields")
		}
	}
}

func TestConcurrentReporting(t *testing.T) {
	var calls atomic.Int32
	r, err := apperror.NewReporter(apperror.ReporterConfig{Sink: apperror.SinkFunc(func(ctx context.Context, record apperror.Record) error {
		calls.Add(1)
		record.Fields["sink"] = "ok"
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Capture(context.Background(), errOrder, apperror.Fields{"shared": "value"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 32 {
		t.Fatal("reports missing")
	}
}
