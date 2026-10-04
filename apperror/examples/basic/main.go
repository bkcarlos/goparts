package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/bkcarlos/goparts/apperror"
)

var ErrOrderNotFound = apperror.New("order.not_found", "订单不存在")

// Embedding Base promotes the standard error and reporting interfaces.
type OrderError struct {
	*apperror.Base
	OrderID string
}

// Override ErrorFields to include domain-specific attributes in reports.
func (e *OrderError) ErrorFields() map[string]string {
	fields := e.Base.ErrorFields()
	if fields == nil {
		fields = map[string]string{}
	}
	fields["order_id"] = e.OrderID
	return fields
}

func findOrder(id string) error {
	return &OrderError{Base: ErrOrderNotFound.With(), OrderID: id}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// This sink writes locally. Replace it with a monitoring/Feishu adapter in
	// the application; this independent module imports no reporting SDK.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	reporter, err := apperror.NewReporter(apperror.ReporterConfig{
		Sink: apperror.SinkFunc(func(ctx context.Context, record apperror.Record) error {
			logger.ErrorContext(ctx, record.Message,
				"code", record.Code, "fields", record.Fields)
			return nil
		}),
	})
	if err != nil {
		return err
	}
	err = findOrder("order-42")
	if errors.Is(err, ErrOrderNotFound) {
		var orderErr *OrderError
		if errors.As(err, &orderErr) {
			fmt.Println("识别业务错误：", orderErr.OrderID)
		}
	}
	wrapped := apperror.Wrap(err, "checkout.failed", "结算失败",
		apperror.WithFields(apperror.Fields{"operation": "checkout"}))
	// Report once at the application boundary; lower layers only wrap/return.
	return reporter.Capture(context.Background(), wrapped,
		apperror.Fields{"request_id": "req-example"})
}
