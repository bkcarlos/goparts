// Package safemap provides concurrent maps. Snapshots copy containers, not values.
package safemap

import "sync"

type SafeMap[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]V
}

func New[K comparable, V any]() *SafeMap[K, V] { return &SafeMap[K, V]{} }
func (m *SafeMap[K, V]) Set(k K, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[K]V{}
	}
	m.data[k] = v
}
func (m *SafeMap[K, V]) Get(k K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[k]
	return v, ok
}
func (m *SafeMap[K, V]) GetOrDefault(k K, fallback V) V {
	if v, ok := m.Get(k); ok {
		return v
	}
	return fallback
}
func (m *SafeMap[K, V]) Delete(k K)   { m.mu.Lock(); delete(m.data, k); m.mu.Unlock() }
func (m *SafeMap[K, V]) Has(k K) bool { _, ok := m.Get(k); return ok }
func (m *SafeMap[K, V]) Len() int     { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.data) }
func (m *SafeMap[K, V]) Clear()       { m.mu.Lock(); m.data = nil; m.mu.Unlock() }
func (m *SafeMap[K, V]) Snapshot() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[K]V, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}
func (m *SafeMap[K, V]) Keys() []K {
	snapshot := m.Snapshot()
	out := make([]K, 0, len(snapshot))
	for k := range snapshot {
		out = append(out, k)
	}
	return out
}
func (m *SafeMap[K, V]) Values() []V {
	snapshot := m.Snapshot()
	out := make([]V, 0, len(snapshot))
	for _, v := range snapshot {
		out = append(out, v)
	}
	return out
}

// Range calls fn outside locks; callbacks may mutate the map.
func (m *SafeMap[K, V]) Range(fn func(K, V) bool) {
	for k, v := range m.Snapshot() {
		if !fn(k, v) {
			return
		}
	}
}

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}
type OrderedSafeMap[K comparable, V any] struct {
	mu    sync.RWMutex
	data  map[K]V
	order []K
}

func NewOrdered[K comparable, V any]() *OrderedSafeMap[K, V] { return &OrderedSafeMap[K, V]{} }
func (m *OrderedSafeMap[K, V]) Set(k K, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[K]V{}
	}
	if _, ok := m.data[k]; !ok {
		m.order = append(m.order, k)
	}
	m.data[k] = v
}
func (m *OrderedSafeMap[K, V]) Get(k K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[k]
	return v, ok
}
func (m *OrderedSafeMap[K, V]) GetOrDefault(k K, fallback V) V {
	if v, ok := m.Get(k); ok {
		return v
	}
	return fallback
}
func (m *OrderedSafeMap[K, V]) Has(k K) bool { _, ok := m.Get(k); return ok }
func (m *OrderedSafeMap[K, V]) Delete(k K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[k]; !ok {
		return
	}
	delete(m.data, k)
	for i, key := range m.order {
		if key == k {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}
func (m *OrderedSafeMap[K, V]) Len() int { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.data) }
func (m *OrderedSafeMap[K, V]) Clear()   { m.mu.Lock(); m.data = nil; m.order = nil; m.mu.Unlock() }
func (m *OrderedSafeMap[K, V]) Snapshot() []Pair[K, V] {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Pair[K, V], 0, len(m.order))
	for _, k := range m.order {
		out = append(out, Pair[K, V]{k, m.data[k]})
	}
	return out
}
func (m *OrderedSafeMap[K, V]) Keys() []K {
	pairs := m.Snapshot()
	out := make([]K, len(pairs))
	for i, p := range pairs {
		out[i] = p.Key
	}
	return out
}
func (m *OrderedSafeMap[K, V]) Values() []V {
	pairs := m.Snapshot()
	out := make([]V, len(pairs))
	for i, p := range pairs {
		out[i] = p.Value
	}
	return out
}
func (m *OrderedSafeMap[K, V]) Range(fn func(K, V) bool) {
	for _, p := range m.Snapshot() {
		if !fn(p.Key, p.Value) {
			return
		}
	}
}
