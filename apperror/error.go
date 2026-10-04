// Package apperror provides coded errors and transport-independent reporting.
package apperror

import (
	"context"
	"errors"
	"strings"
)

const CodeUnknown = "common.unknown"
const CodeCanceled = "common.canceled"
const CodeDeadlineExceeded = "common.deadline_exceeded"

// Coded is structural: other modules can implement it without importing this
// package. ErrorInfo returns a stable code and a short, reportable description.
type Coded interface {
	error
	ErrorInfo() (code, message string)
}

// FieldProvider supplies reportable context. Use identifiers, not credentials.
// Values are strings so copying the map gives a fully independent snapshot.
type FieldProvider interface{ ErrorFields() map[string]string }
type Fields = map[string]string

// Base is immutable after construction. Embed *Base in domain errors to promote
// Error, Unwrap, Is, ErrorInfo and ErrorFields, or implement Coded yourself.
// The name Base avoids a field named Error shadowing the Error() method.
type Base struct {
	code    string
	message string
	cause   error
	fields  Fields
}

type Option func(*Base)

// WithFields snapshots its input immediately, so an option can be reused safely.
func WithFields(fields Fields) Option {
	snapshot := clone(fields)
	return func(e *Base) { e.fields = merge(e.fields, snapshot) }
}

// New constructs an error or reusable definition. Blank values get defaults.
func New(code, message string, options ...Option) *Base {
	code = strings.TrimSpace(code)
	if code == "" {
		code = CodeUnknown
	}
	if strings.TrimSpace(message) == "" {
		message = "unspecified error"
	}
	e := &Base{code: code, message: message}
	for _, option := range options {
		if option != nil {
			option(e)
		}
	}
	return e
}

func (e *Base) Code() string                   { return e.code }
func (e *Base) Message() string                { return e.message }
func (e *Base) ErrorInfo() (string, string)    { return e.code, e.message }
func (e *Base) ErrorFields() map[string]string { return clone(e.fields) }
func (e *Base) Unwrap() error                  { return e.cause }

// Error includes the cause for normal Go diagnostics. Structured reports omit
// this full detail unless ReporterConfig.IncludeDetail is explicitly enabled.
func (e *Base) Error() string {
	text := "[" + e.code + "] " + e.message
	if e.cause != nil {
		text += ": " + e.cause.Error()
	}
	return text
}

// Is matches a coded target by code, not description/fields. Unknown errors do
// not match by code; normal pointer equality and cause traversal still apply.
func (e *Base) Is(target error) bool {
	coded, ok := target.(Coded)
	if !ok || e.code == CodeUnknown || e.code == "" {
		return false
	}
	code, _ := coded.ErrorInfo()
	return e.code == code
}

// With returns a copy with additional context. It never changes a shared error
// definition. Options override existing fields with the same keys.
func (e *Base) With(options ...Option) *Base {
	copy := *e
	copy.fields = clone(e.fields)
	for _, option := range options {
		if option != nil {
			option(&copy)
		}
	}
	return &copy
}

// Wrap preserves cause for errors.Is/As and adds an outer classification.
// A nil cause returns a nil error interface, so `return definition.Wrap(err)`
// is safe on success. Cause fields < definition fields < call-site options.
func (e *Base) Wrap(cause error, options ...Option) error {
	if cause == nil {
		return nil
	}
	copy := *e
	copy.cause = cause
	copy.fields = merge(Describe(cause).Fields, e.fields)
	for _, option := range options {
		if option != nil {
			option(&copy)
		}
	}
	return &copy
}

func Wrap(cause error, code, message string, options ...Option) error {
	if cause == nil {
		return nil
	}
	return New(code, message).Wrap(cause, options...)
}

// CodeOf selects the first Coded error in errors.As traversal order (outermost,
// depth-first for errors.Join). nil has no code. Standard context errors have
// built-in codes; other uncoded errors use CodeUnknown.
func CodeOf(err error) string {
	if report := Describe(err); report != nil {
		return report.Code
	}
	return ""
}

// Describe creates a fresh snapshot. For errors.Join it selects the first coded
// branch, without merging unrelated sibling fields. Report branches separately
// if each joined error needs its own classification.
func Describe(err error) *Record {
	if err == nil {
		return nil
	}
	r := &Record{Code: CodeUnknown, Message: "unclassified error"}
	var coded Coded
	if errors.As(err, &coded) {
		r.Code, r.Message = coded.ErrorInfo()
		if strings.TrimSpace(r.Code) == "" {
			r.Code = CodeUnknown
		}
		if strings.TrimSpace(r.Message) == "" {
			r.Message = "unspecified error"
		}
		if provider, ok := coded.(FieldProvider); ok {
			r.Fields = clone(provider.ErrorFields())
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		r.Code, r.Message = CodeDeadlineExceeded, "operation deadline exceeded"
	} else if errors.Is(err, context.Canceled) {
		r.Code, r.Message = CodeCanceled, "operation canceled"
	}
	return r
}

func clone(fields Fields) Fields {
	if len(fields) == 0 {
		return nil
	}
	copy := make(Fields, len(fields))
	for k, v := range fields {
		copy[k] = v
	}
	return copy
}
func merge(base, extra Fields) Fields {
	if len(base)+len(extra) == 0 {
		return nil
	}
	result := make(Fields, len(base)+len(extra))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}
