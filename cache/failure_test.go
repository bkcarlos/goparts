package cache

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStoreCancellationAndDeleteContract(t *testing.T) {
	for _, kind := range []string{"memory", "file"} {
		t.Run(kind, func(t *testing.T) {
			var s Store
			var err error
			if kind == "memory" {
				s, err = NewMemory(2)
			} else {
				s, err = NewFile(t.TempDir(), 4)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := s.Set(ctx, "key", []byte("old"), 0); err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := s.Set(canceled, "key", []byte("new"), 0); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if err := s.Delete(canceled, "key"); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, hit, err := s.Get(canceled, "key"); hit || !errors.Is(err, context.Canceled) {
				t.Fatalf("hit=%v err=%v", hit, err)
			}
			if err := s.Set(ctx, "key", nil, -time.Second); err == nil {
				t.Fatal("negative TTL accepted")
			}
			v, hit, err := s.Get(ctx, "key")
			if err != nil || !hit || string(v) != "old" {
				t.Fatalf("value mutated: %q %v %v", v, hit, err)
			}
			for i := 0; i < 2; i++ {
				if err := s.Delete(ctx, "key"); err != nil {
					t.Fatal(err)
				}
			}
			if _, hit, err := s.Get(ctx, "key"); hit || err != nil {
				t.Fatalf("delete failed: %v %v", hit, err)
			}
		})
	}
}

func TestFileRejectsCorruptionAndOversizedData(t *testing.T) {
	f, err := NewFile(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data string }{
		{"malformed", `{"value":`}, {"invalid base64", `{"value":"!"}`},
		{"decoded size", `{"value":"MTIzNDU="}`}, {"encoded size", strings.Repeat(" ", 1032)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(f.path("key"), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, hit, err := f.Get(context.Background(), "key"); hit || err == nil {
				t.Fatalf("corrupt entry accepted: %v %v", hit, err)
			}
		})
	}
	if err := f.Set(context.Background(), "key", []byte("12345"), 0); err == nil {
		t.Fatal("oversize accepted")
	}
	if err := f.Set(context.Background(), "key", []byte("1234"), 0); err != nil {
		t.Fatal(err)
	}
	v, hit, err := f.Get(context.Background(), "key")
	if err != nil || !hit || string(v) != "1234" {
		t.Fatalf("repair failed: %q %v %v", v, hit, err)
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v %v", entries, err)
	}
}

type failingStore struct {
	Store
	getErr, setErr error
}

func (s failingStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	return s.Store.Get(ctx, key)
}
func (s failingStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	return s.Store.Set(ctx, key, v, ttl)
}

func TestLoaderFailuresDoNotPoisonNextAttempt(t *testing.T) {
	for _, kind := range []string{"get", "set", "fetch", "panic"} {
		t.Run(kind, func(t *testing.T) {
			m, err := NewMemory(2)
			if err != nil {
				t.Fatal(err)
			}
			want := errors.New("backend failure")
			store := failingStore{Store: m}
			if kind == "get" {
				store.getErr = want
			}
			if kind == "set" {
				store.setErr = want
			}
			l := &Loader{Store: store}
			calls := 0
			_, err = l.LoadOrFetch(context.Background(), "key", 0, func(context.Context) ([]byte, error) {
				calls++
				if kind == "panic" {
					panic("boom")
				}
				if kind == "fetch" {
					return nil, want
				}
				return []byte("value"), nil
			})
			if err == nil || (kind != "panic" && !errors.Is(err, want)) {
				t.Fatalf("error lost: %v", err)
			}
			if kind == "get" && calls != 0 {
				t.Fatal("fetch called after store error")
			}
			l.Store = m
			v, err := l.LoadOrFetch(context.Background(), "key", 0, func(context.Context) ([]byte, error) { return []byte("retry"), nil })
			if err != nil || string(v) != "retry" {
				t.Fatalf("retry failed: %q %v", v, err)
			}
			v[0] = 'x'
			v, err = l.LoadOrFetch(context.Background(), "key", 0, func(context.Context) ([]byte, error) { t.Error("cache miss"); return nil, nil })
			if err != nil || string(v) != "retry" {
				t.Fatalf("aliased result: %q %v", v, err)
			}
		})
	}
}
