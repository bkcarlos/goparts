package otel

import (
	"context"
	"github.com/bkcarlos/goparts/metrics"
	"go.opentelemetry.io/otel/metric/noop"
	"testing"
)

func TestAdapters(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	c, err := Counter(meter, "requests")
	if err != nil {
		t.Fatal(err)
	}
	h, err := Histogram(meter, "duration")
	if err != nil {
		t.Fatal(err)
	}
	g, err := Gauge(meter, "queued")
	if err != nil {
		t.Fatal(err)
	}
	labels := metrics.Labels{"operation": "get"}
	ctx := context.Background()
	c.Add(ctx, 1, labels)
	h.Record(ctx, 0.1, labels)
	g.Set(ctx, 3, labels)
	if attrs := attributes(metrics.Labels{"b": "2", "a": "1"}); string(attrs[0].Key) != "a" {
		t.Fatal(attrs)
	}
}
