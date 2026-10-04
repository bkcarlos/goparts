package apperror

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// Record is JSON-ready and independent from the original error object.
type Record struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fields  Fields `json:"fields,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type Sink interface {
	Report(context.Context, Record) error
}
type SinkFunc func(context.Context, Record) error

func (f SinkFunc) Report(ctx context.Context, record Record) error {
	if f == nil {
		return errors.New("apperror: nil SinkFunc")
	}
	return f(ctx, record)
}

type ReporterConfig struct {
	Sink          Sink
	IncludeDetail bool // default false; true includes err.Error(), including causes
}

// Reporter synchronously submits one record per Capture call. It does not retry,
// deduplicate, queue or initiate background work. A shared Sink must be safe for
// concurrent calls and respect the supplied context.
type Reporter struct {
	sink          Sink
	includeDetail bool
}

func NewReporter(cfg ReporterConfig) (*Reporter, error) {
	if cfg.Sink == nil {
		return nil, errors.New("apperror: reporting Sink is required")
	}
	v := reflect.ValueOf(cfg.Sink)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil, errors.New("apperror: reporting Sink is required")
		}
	}
	return &Reporter{sink: cfg.Sink, includeDetail: cfg.IncludeDetail}, nil
}

// Capture(nil error) does nothing. Fields supplied here override error fields.
// The returned error is the reporting failure, not the original business error;
// callers should handle it separately and retain their original error.
func (r *Reporter) Capture(ctx context.Context, err error, fields ...Fields) error {
	if err == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("apperror: context is required")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	record := Describe(err)
	for _, extra := range fields {
		record.Fields = merge(record.Fields, extra)
	}
	if r.includeDetail {
		record.Detail = err.Error()
	}
	if reportErr := r.sink.Report(ctx, *record); reportErr != nil {
		return fmt.Errorf("apperror: report failed: %w", reportErr)
	}
	return nil
}
