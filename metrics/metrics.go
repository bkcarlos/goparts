// Package metrics defines backend-neutral instruments. Labels must have bounded
// cardinality; do not put user IDs, tokens or complete URLs in them.
package metrics

import "context"

type Labels map[string]string
type Counter interface {
	Add(context.Context, int64, Labels)
}
type Histogram interface {
	Record(context.Context, float64, Labels)
}
type Gauge interface {
	Set(context.Context, float64, Labels)
}
type Nop struct{}

func (Nop) Add(context.Context, int64, Labels)      {}
func (Nop) Record(context.Context, float64, Labels) {}
func (Nop) Set(context.Context, float64, Labels)    {}
