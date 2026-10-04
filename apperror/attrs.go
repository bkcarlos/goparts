package apperror

import (
	"encoding/json"
	"log/slog"
)

type AttrProvider interface{ ErrorAttrs() map[string]any }

func cloneAttrs(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return map[string]any{"attribute_error": "unserializable attributes"}
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func WithAttrs(attrs ...slog.Attr) Option {
	values := map[string]any{}
	var add func(map[string]any, slog.Attr)
	add = func(dst map[string]any, a slog.Attr) {
		v := a.Value.Resolve()
		if v.Kind() == slog.KindGroup {
			group := map[string]any{}
			for _, child := range v.Group() {
				add(group, child)
			}
			if a.Key == "" {
				for k, v := range group {
					dst[k] = v
				}
			} else {
				dst[a.Key] = group
			}
		} else if a.Key != "" {
			dst[a.Key] = v.Any()
		}
	}
	for _, a := range attrs {
		add(values, a)
	}
	snapshot := cloneAttrs(values)
	return func(e *Base) {
		if e.attrs == nil {
			e.attrs = map[string]any{}
		}
		for k, v := range cloneAttrs(snapshot) {
			e.attrs[k] = v
		}
	}
}
func (e *Base) ErrorAttrs() map[string]any { return cloneAttrs(e.attrs) }
