package integration_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bkcarlos/goparts/apperror"
	"github.com/bkcarlos/goparts/httpclient"
	"github.com/bkcarlos/goparts/logger"
	"github.com/bkcarlos/goparts/retry"
)

func TestHTTPRetryReportOnceToLogger(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.Header().Set("Set-Cookie", "private-cookie")
		w.WriteHeader(503)
	}))
	defer srv.Close()
	client, err := httpclient.New(httpclient.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	r, err := retry.New(retry.Config{InitialDelay: time.Nanosecond, RetryIf: func(err error) bool {
		var status *httpclient.StatusError
		return errors.As(err, &status) && status.Method == "GET" && status.StatusCode == 503
	}, RetryAfter: func(err error) (time.Duration, bool) {
		var status *httpclient.StatusError
		if errors.As(err, &status) {
			return status.RetryDelay(time.Now())
		}
		return 0, false
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = r.Do(context.Background(), func(ctx context.Context) error {
		_, err := client.Do(ctx, httpclient.Request{URL: srv.URL})
		return err
	})
	if attempts.Load() != 3 || err == nil {
		t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
	}
	var output bytes.Buffer
	l, logErr := logger.New(logger.Config{Writer: &output})
	if logErr != nil {
		t.Fatal(logErr)
	}
	reporter, reportErr := apperror.NewReporter(apperror.ReporterConfig{Sink: apperror.SinkFunc(func(ctx context.Context, r apperror.Record) error {
		l.ErrorContext(ctx, r.Message, "code", r.Code, "fields", r.Fields)
		return nil
	})})
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	if reportErr = reporter.Capture(context.Background(), err); reportErr != nil {
		t.Fatal(reportErr)
	}
	if strings.Count(output.String(), "\n") != 1 || !strings.Contains(output.String(), "httpclient.http_status") || strings.Contains(output.String(), "private-cookie") {
		t.Fatalf("log=%s", output.String())
	}
}
