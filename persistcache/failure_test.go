package persistcache

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRejectsCorruptPersistentFiles(t *testing.T) {
	for _, data := range []string{`{`, `{"bad-int":"2026-01-01T00:00:00Z"}`, `{"1":"bad-time"}`} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := New(path, Int)
			if err == nil {
				c.Close()
				t.Fatal("corrupt cache accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("corrupt file modified: %q %v", got, err)
			}
		})
	}
}

func TestSerializerFailurePreservesPublishedCache(t *testing.T) {
	for _, kind := range []string{"encode error", "collision"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.json")
			original := []byte(`{"old":"2026-01-01T00:00:00Z"}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			want := errors.New("encode failed")
			serializer := Serializer[string]{Decode: String.Decode, Encode: func(string) (string, error) {
				if kind == "encode error" {
					return "", want
				}
				return "same", nil
			}}
			c, err := New(path, serializer)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.MarkUpdated("new"); err != nil {
				t.Fatal(err)
			}
			err = c.Close()
			if err == nil || kind == "encode error" && !errors.Is(err, want) {
				t.Fatalf("close error=%v", err)
			}
			if c.LastError() == nil {
				t.Fatal("background save error lost")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(original) {
				t.Fatalf("old cache lost: %q %v", got, err)
			}
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(files) != 1 {
				t.Fatalf("temporary file leak: %v %v", files, err)
			}
		})
	}
}

func TestConcurrentCloseFlushesAndRejectsMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache.json")
	c, err := New(path, Int64)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 20; i++ {
		if err := c.MarkUpdated(i); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, fn := range []func() error{func() error { return c.MarkUpdated(50) }, func() error { return c.Delete(1) }, c.Clear} {
		if err := fn(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("mutation after close: %v", err)
		}
	}
	loaded, err := New(path, Int64)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.Len() != 20 || len(loaded.Keys()) != 20 || loaded.ShouldUpdate(1, time.Hour) {
		t.Fatal("close did not flush all timestamps")
	}
	if !loaded.ShouldUpdate(1, 0) {
		t.Fatal("zero expiry should force update")
	}
	if err := loaded.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(); err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 0 || loaded.Has(1) {
		t.Fatal("clear failed")
	}
}
