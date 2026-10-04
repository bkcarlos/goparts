// Package otel adapts the OpenTelemetry metric API without installing providers.
package otel

import (
	"context"
	"github.com/bkcarlos/goparts/metrics"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"sort"
)

func attributes(labels metrics.Labels) []attribute.KeyValue {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]attribute.KeyValue, 0, len(keys))
	for _, key := range keys {
		out = append(out, attribute.String(key, labels[key]))
	}
	return out
}

type counter struct{ instrument otelmetric.Int64Counter }

func (c counter) Add(ctx context.Context, value int64, labels metrics.Labels) {
	c.instrument.Add(ctx, value, otelmetric.WithAttributes(attributes(labels)...))
}
func Counter(meter otelmetric.Meter, name string) (metrics.Counter, error) {
	c, err := meter.Int64Counter(name)
	if err != nil {
		return nil, err
	}
	return counter{c}, nil
}

type histogram struct{ instrument otelmetric.Float64Histogram }

func (h histogram) Record(ctx context.Context, value float64, labels metrics.Labels) {
	h.instrument.Record(ctx, value, otelmetric.WithAttributes(attributes(labels)...))
}
func Histogram(meter otelmetric.Meter, name string) (metrics.Histogram, error) {
	h, err := meter.Float64Histogram(name)
	if err != nil {
		return nil, err
	}
	return histogram{h}, nil
}

type gauge struct{ instrument otelmetric.Float64Gauge }

func (g gauge) Set(ctx context.Context, value float64, labels metrics.Labels) {
	g.instrument.Record(ctx, value, otelmetric.WithAttributes(attributes(labels)...))
}
func Gauge(meter otelmetric.Meter, name string) (metrics.Gauge, error) {
	g, err := meter.Float64Gauge(name)
	if err != nil {
		return nil, err
	}
	return gauge{g}, nil
}
