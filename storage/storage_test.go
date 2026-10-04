package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A second, SDK-free backend proves that callers only rely on the contract.
type memoryBackend struct {
	data      map[string]string
	lastKey   string
	list      ListOptions
	statError error
}

func (m *memoryBackend) Put(_ context.Context, key string, r io.Reader, size int64, _ PutOptions) (Object, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Object{}, err
	}
	m.data[key] = string(b)
	m.lastKey = key
	return Object{Size: size}, nil
}
func (m *memoryBackend) Get(_ context.Context, key string, _ GetOptions) (*Reader, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return &Reader{ReadCloser: io.NopCloser(strings.NewReader(v)), Object: Object{Size: int64(len(v))}, Length: int64(len(v))}, nil
}
func (m *memoryBackend) Stat(_ context.Context, key string) (Object, error) {
	if m.statError != nil {
		return Object{}, m.statError
	}
	v, ok := m.data[key]
	if !ok {
		return Object{}, ErrNotFound
	}
	return Object{Size: int64(len(v))}, nil
}
func (m *memoryBackend) Delete(_ context.Context, key string) error { delete(m.data, key); return nil }
func (m *memoryBackend) List(_ context.Context, opts ListOptions) (Page, error) {
	m.list = opts
	page := Page{NextCursor: "opaque-next"}
	for k := range m.data {
		if strings.HasPrefix(k, opts.Prefix) {
			page.Objects = append(page.Objects, Object{Key: k})
		}
	}
	return page, nil
}
func (m *memoryBackend) PresignGet(_ context.Context, key string, ttl time.Duration) (SignedURL, error) {
	m.lastKey = key
	return SignedURL{URL: "https://example.com/signed", ExpiresAt: time.Now().Add(ttl)}, nil
}

func TestProviderNeutralNamespaceAndFiles(t *testing.T) {
	m := &memoryBackend{data: map[string]string{}}
	c, err := New(m, Config{BasePath: "releases/app", MaxReadBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	progress := int64(0)
	o, err := c.Put(ctx, "bin/a", strings.NewReader("abc"), 3, PutOptions{OnProgress: func(read, total int64) error { progress = read; return nil }})
	if err != nil || o.Key != "bin/a" || m.lastKey != "releases/app/bin/a" || progress != 3 {
		t.Fatalf("object=%+v err=%v", o, err)
	}
	b, err := c.ReadAll(ctx, "bin/a")
	if err != nil || string(b) != "abc" {
		t.Fatal(err)
	}
	page, err := c.List(ctx, ListOptions{Prefix: "bin/", Cursor: "opaque", Limit: 2})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "bin/a" || m.list.Prefix != "releases/app/bin/" || m.list.Cursor != "opaque" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err = c.PresignGet(ctx, "bin/a", time.Minute); err != nil || m.lastKey != "releases/app/bin/a" {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "large")
	if err = os.WriteFile(path, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.PutFile(ctx, "large", path, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReadAll(ctx, "large"); err == nil {
		t.Fatal("read cap ignored")
	}
	if err = c.Delete(ctx, "bin/a"); err != nil {
		t.Fatal(err)
	}
	if exists, err := c.Exists(ctx, "bin/a"); exists || err != nil {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	m.statError = ErrPermission
	if _, err := c.Exists(ctx, "bin/a"); !errors.Is(err, ErrPermission) {
		t.Fatal("permission error hidden")
	}
}
func TestSourceValidationAndErrors(t *testing.T) {
	m := &memoryBackend{data: map[string]string{}}
	c, _ := New(m, Config{BasePath: "base"})
	ctx := context.Background()
	for _, key := range []string{"", "../escape", "a/../../b", "/absolute", "a//b", "a\\b"} {
		if _, err := c.Put(ctx, key, strings.NewReader("a"), 1, PutOptions{}); err == nil {
			t.Errorf("accepted key %q", key)
		}
	}
	for _, size := range []int64{2, 4} {
		if _, err := c.Put(ctx, "a", strings.NewReader("abc"), size, PutOptions{}); !errors.Is(err, ErrSizeMismatch) {
			t.Fatalf("size=%d err=%v", size, err)
		}
	}
	want := errors.New("abort")
	if _, err := c.Put(ctx, "a", strings.NewReader("a"), 1, PutOptions{OnProgress: func(int64, int64) error { return want }}); !errors.Is(err, want) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Stat(canceled, "a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "a", GetOptions{Offset: -1}); err == nil {
		t.Fatal("negative range")
	}
	if _, err := New(m, Config{BasePath: "../escape"}); err == nil {
		t.Fatal("invalid base path")
	}
	for _, base := range []string{"/", "base//"} {
		if _, err := New(m, Config{BasePath: base}); err == nil {
			t.Fatalf("invalid base accepted %q", base)
		}
	}
	if _, err := c.List(ctx, ListOptions{Prefix: "a//"}); err == nil {
		t.Fatal("invalid list prefix accepted")
	}
	if _, err := c.Put(ctx, "empty", strings.NewReader(""), 0, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	e := &Error{Provider: "mock", Operation: "get", Cause: want, Kind: ErrNotFound}
	if !errors.Is(e, want) || !errors.Is(e, ErrNotFound) || strings.Contains(e.Error(), want.Error()) {
		t.Fatal("error mapping")
	}
}
