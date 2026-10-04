package persistcache

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPersistenceAndConcurrentClose(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	c, err := New(p, Int)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		i := i
		wg.Add(1)
		go func() { defer wg.Done(); c.MarkUpdated(i) }()
	}
	wg.Wait()
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(p, Int)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.Len() != 30 || loaded.ShouldUpdate(1, time.Hour) || !loaded.ShouldUpdate(31, time.Hour) {
		t.Fatal("persistence")
	}
	loaded.Delete(1)
	if loaded.Has(1) {
		t.Fatal("delete")
	}
	if c.MarkUpdated(99) == nil {
		t.Fatal("closed")
	}
}
