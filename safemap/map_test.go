package safemap

import (
	"sync"
	"testing"
)

func TestConcurrentSnapshotAndOrdered(t *testing.T) {
	var m SafeMap[int, int]
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() { defer wg.Done(); m.Set(i, i); m.Snapshot() }()
	}
	wg.Wait()
	if m.Len() != 100 {
		t.Fatal(m.Len())
	}
	o := NewOrdered[string, int]()
	o.Set("b", 1)
	o.Set("a", 2)
	o.Set("b", 3)
	o.Delete("a")
	o.Set("a", 4)
	keys := o.Keys()
	if len(keys) != 2 || keys[0] != "b" || keys[1] != "a" {
		t.Fatal(keys)
	}
	o.Range(func(k string, v int) bool { o.Delete(k); return true })
	if o.Len() != 0 {
		t.Fatal(o.Len())
	}
}
