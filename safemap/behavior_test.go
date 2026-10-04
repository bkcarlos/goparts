package safemap

import (
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestSnapshotIsolationAndReentrantRange(t *testing.T) {
	m := New[string, int]()
	if m.Has("missing") || m.GetOrDefault("missing", 42) != 42 {
		t.Fatal("missing key contract")
	}
	m.Set("a", 0)
	m.Set("b", 2)
	if m.GetOrDefault("a", 42) != 0 {
		t.Fatal("zero value confused with missing key")
	}
	snapshot := m.Snapshot()
	snapshot["a"] = 99
	delete(snapshot, "b")
	if v, ok := m.Get("a"); !ok || v != 0 || m.Len() != 2 {
		t.Fatal("snapshot aliases map")
	}
	keys := m.Keys()
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatal(keys)
	}
	values := m.Values()
	sort.Ints(values)
	if !reflect.DeepEqual(values, []int{0, 2}) {
		t.Fatal(values)
	}
	calls := 0
	m.Range(func(k string, v int) bool { calls++; m.Delete(k); m.Set("new", 3); return false })
	if calls != 1 {
		t.Fatalf("early stop ignored: %d", calls)
	}
	m.Clear()
	m.Delete("missing")
	if m.Len() != 0 {
		t.Fatal("clear failed")
	}
	m.Set("reuse", 1)
	if !m.Has("reuse") {
		t.Fatal("reuse after clear failed")
	}
}

func TestOrderedMapConcurrentSnapshotsPreserveKeyValuePairs(t *testing.T) {
	m := NewOrdered[int, int]()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Set(i, i*10)
			for _, p := range m.Snapshot() {
				if p.Value != p.Key*10 {
					t.Errorf("inconsistent pair: %+v", p)
				}
			}
		}()
	}
	wg.Wait()
	if m.Len() != 64 {
		t.Fatal(m.Len())
	}
	keys, values := m.Keys(), m.Values()
	for i, k := range keys {
		if values[i] != k*10 {
			t.Fatal("key/value order mismatch")
		}
	}
	snapshot := m.Snapshot()
	first := snapshot[0].Key
	snapshot[0].Value = -1
	if m.GetOrDefault(first, -1) != first*10 || !m.Has(first) {
		t.Fatal("snapshot mutated map")
	}
	before := m.Keys()
	m.Set(first, 999)
	if !reflect.DeepEqual(before, m.Keys()) {
		t.Fatal("update changed insertion order")
	}
	m.Delete(first)
	m.Set(first, first*10)
	if m.Keys()[63] != first {
		t.Fatal("reinsert did not append")
	}
	count := 0
	m.Range(func(k, v int) bool { count++; m.Delete(k); return false })
	if count != 1 {
		t.Fatal("early stop ignored")
	}
	m.Delete(-1)
	m.Clear()
	if m.Len() != 0 || m.Has(first) || m.GetOrDefault(first, -1) != -1 || len(m.Values()) != 0 {
		t.Fatal("clear failed")
	}
	m.Set(1, 10)
	if m.GetOrDefault(1, -1) != 10 {
		t.Fatal("reuse failed")
	}
}
