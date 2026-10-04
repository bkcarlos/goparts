package apperror_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/bkcarlos/goparts/apperror"
)

var errOrder = apperror.New("order.not_found", "订单不存在")

type OrderError struct {
	*apperror.Base
	OrderID string
}

func (e *OrderError) ErrorFields() map[string]string {
	fields := e.Base.ErrorFields()
	if fields == nil {
		fields = map[string]string{}
	}
	fields["order_id"] = e.OrderID
	return fields
}

// This models an independent library that does not embed Base.
type upstreamError struct{ status int }

func (e upstreamError) Error() string               { return fmt.Sprintf("upstream status=%d", e.status) }
func (e upstreamError) ErrorInfo() (string, string) { return "upstream.failed", "上游调用失败" }
func (e upstreamError) ErrorFields() map[string]string {
	return map[string]string{"status": fmt.Sprint(e.status)}
}

var _ error = (*OrderError)(nil)
var _ apperror.Coded = (*OrderError)(nil)
var _ apperror.Coded = upstreamError{}

func TestWrapPreservesIdentityAndNil(t *testing.T) {
	if apperror.Wrap(nil, "code", "message") != nil || errOrder.Wrap(nil) != nil {
		t.Fatal("nil became non-nil error")
	}
	cause := &os.PathError{Op: "open", Path: "private", Err: os.ErrNotExist}
	err := errOrder.Wrap(cause, apperror.WithFields(apperror.Fields{"order_id": "42"}))
	if !errors.Is(err, errOrder) || !errors.Is(err, os.ErrNotExist) {
		t.Fatal("classification or cause lost")
	}
	var original *os.PathError
	if !errors.As(err, &original) || original != cause {
		t.Fatal("concrete cause lost")
	}
	if !errors.Is(err, apperror.New("order.not_found", "different wording")) {
		t.Fatal("same code did not match")
	}
	if errors.Is(err, apperror.New("other", "订单不存在")) {
		t.Fatal("different code matched")
	}
	if errors.Is(apperror.New("", "a"), apperror.New("", "b")) {
		t.Fatal("unknown errors matched by code")
	}
	if err.Error() != "[order.not_found] 订单不存在: open private: file does not exist" {
		t.Fatalf("error=%s", err)
	}
}

func TestFieldsAreSnapshotsAndDefinitionsReusable(t *testing.T) {
	input := apperror.Fields{"operation": "original", "collision": "option"}
	opt := apperror.WithFields(input)
	input["operation"] = "mutated"
	definition := apperror.New("orders.failed", "订单处理失败", apperror.WithFields(apperror.Fields{"collision": "definition"}))
	inner := apperror.New("db.failed", "查询失败", apperror.WithFields(apperror.Fields{"collision": "cause", "db": "orders"}))
	err := definition.Wrap(inner, opt)
	r := apperror.Describe(err)
	if r.Code != "orders.failed" || r.Fields["collision"] != "option" || r.Fields["operation"] != "original" || r.Fields["db"] != "orders" {
		t.Fatalf("report=%+v", r)
	}
	r.Fields["collision"] = "changed"
	if apperror.Describe(err).Fields["collision"] != "option" {
		t.Fatal("report mutated original")
	}
	if definition.ErrorFields()["collision"] != "definition" || len(definition.ErrorFields()) != 1 {
		t.Fatal("definition mutated")
	}
	copy := definition.With(opt)
	if copy == definition || copy.ErrorFields()["operation"] != "original" {
		t.Fatal("With did not copy")
	}
	fields := copy.ErrorFields()
	fields["operation"] = "changed"
	if copy.ErrorFields()["operation"] != "original" {
		t.Fatal("accessor exposed internal map")
	}
	if definition.Code() != "orders.failed" || definition.Message() != "订单处理失败" {
		t.Fatal("accessors")
	}
	if empty := apperror.New(" ", " ", nil); empty.Code() != apperror.CodeUnknown || empty.Message() == "" {
		t.Fatal("missing defaults")
	}
}

func TestEmbeddingCompositionAndJoinedErrors(t *testing.T) {
	domain := &OrderError{Base: errOrder.With(), OrderID: "42"}
	err := fmt.Errorf("controller: %w", domain)
	var found *OrderError
	if !errors.As(err, &found) || found.OrderID != "42" || !errors.Is(err, errOrder) {
		t.Fatal("embedding broke error contract")
	}
	r := apperror.Describe(err)
	if r.Code != "order.not_found" || r.Fields["order_id"] != "42" {
		t.Fatalf("embedded error: %+v", r)
	}
	joined := errors.Join(errors.New("plain"), domain, upstreamError{503})
	r = apperror.Describe(joined)
	if r.Code != "order.not_found" || r.Fields["status"] != "" {
		t.Fatalf("join merged branches: %+v", r)
	}
	outer := apperror.Wrap(upstreamError{503}, "checkout.failed", "结算失败")
	var upstream upstreamError
	if !errors.As(outer, &upstream) || upstream.status != 503 || apperror.Describe(outer).Fields["status"] != "503" {
		t.Fatal("composition lost upstream")
	}
	if apperror.CodeOf(upstreamError{500}) != "upstream.failed" {
		t.Fatal("structural interface not recognized")
	}
}

func TestFallbacks(t *testing.T) {
	if apperror.Describe(nil) != nil || apperror.CodeOf(nil) != "" {
		t.Fatal("nil error has a report")
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{errors.New("password=secret"), apperror.CodeUnknown},
		{fmt.Errorf("request: %w", context.Canceled), apperror.CodeCanceled},
		{context.DeadlineExceeded, apperror.CodeDeadlineExceeded},
		{apperror.Wrap(context.Canceled, "job.stopped", "任务停止"), "job.stopped"},
	} {
		r := apperror.Describe(tc.err)
		if r.Code != tc.code || r.Message == "password=secret" || r.Detail != "" {
			t.Fatalf("report=%+v", r)
		}
	}
}

func TestConcurrentDefinitionReuse(t *testing.T) {
	opt := apperror.WithFields(apperror.Fields{"service": "orders"})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := errOrder.Wrap(upstreamError{404}, opt, apperror.WithFields(apperror.Fields{"id": fmt.Sprint(i)}))
			r := apperror.Describe(err)
			if r.Fields["id"] != fmt.Sprint(i) || r.Fields["service"] != "orders" {
				t.Errorf("context crossed calls: %+v", r)
			}
		}(i)
	}
	wg.Wait()
	if len(errOrder.ErrorFields()) != 0 {
		t.Fatal("shared definition changed")
	}
}
