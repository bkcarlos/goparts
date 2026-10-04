package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func source(data string, size int64) Source {
	return SourceFunc(func(context.Context) (Stream, error) {
		return Stream{Body: io.NopCloser(strings.NewReader(data)), Size: size}, nil
	})
}
func TestAtomicPublicationAndChecksum(t *testing.T) {
	c, _ := New(Config{})
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	sum := sha256.Sum256([]byte("hello"))
	checksum := hex.EncodeToString(sum[:])
	progress := int64(0)
	result, err := c.Fetch(ctx, source("hello", 5), path, Options{SHA256: checksum, OnProgress: func(written, total int64) error {
		progress = written
		if total != 5 {
			t.Error("total lost")
		}
		return nil
	}})
	if err != nil || result.Bytes != 5 || result.SHA256 != checksum || progress != 5 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := c.Fetch(ctx, source("new", 3), path, Options{}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := c.Fetch(ctx, source("bad", 3), path, Options{Overwrite: true, SHA256: checksum}); !errors.Is(err, ErrChecksum) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Fatal("failed download overwrote target")
	}
	if _, err := c.Fetch(ctx, source("new", 3), path, Options{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "new" {
		t.Fatal("replacement failed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary files leaked")
	}
}
func TestFailureCleanupLimitsCancellationAndRace(t *testing.T) {
	want := errors.New("progress canceled")
	for _, tc := range []struct {
		name, data string
		size, cap  int64
		opts       Options
		want       error
	}{
		{"too large", "1234", -1, 3, Options{}, ErrTooLarge},
		{"short", "12", 3, 10, Options{}, ErrSizeMismatch},
		{"long", "1234", 3, 10, Options{}, ErrSizeMismatch},
		{"callback", "123", 3, 10, Options{OnProgress: func(int64, int64) error { return want }}, want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := New(Config{MaxBytes: tc.cap, BufferSize: 2})
			dir := t.TempDir()
			_, err := c.Fetch(context.Background(), source(tc.data, tc.size), filepath.Join(dir, "file"), tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v", err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("partial file published")
			}
		})
	}
	c, _ := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Fetch(ctx, source("a", 1), filepath.Join(t.TempDir(), "x"), Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	_, err := c.Fetch(context.Background(), source("new", 3), path, Options{OnProgress: func(int64, int64) error { return os.WriteFile(path, []byte("other writer"), 0600) }})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("race result=%v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "other writer" {
		t.Fatal("concurrent writer overwritten")
	}
}
func TestHTTPSourceAndRedirectErrors(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file":
			if r.Header.Get("Authorization") != "Bearer test" {
				t.Error("headers lost")
			}
			io.WriteString(w, "hello")
		case "/redirect":
			http.Redirect(w, r, "/target", 302)
		case "/target":
			hits++
			io.WriteString(w, "secret")
		default:
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()
	c, _ := New(Config{})
	s, err := NewHTTPSource(srv.URL+"/file", HTTPConfig{Headers: http.Header{"Authorization": {"Bearer test"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Fetch(context.Background(), s, filepath.Join(t.TempDir(), "f"), Options{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/redirect", "/error?secret=private"} {
		s, _ := NewHTTPSource(srv.URL+path, HTTPConfig{})
		_, err := c.Fetch(context.Background(), s, filepath.Join(t.TempDir(), "f"), Options{})
		var status *HTTPError
		if !errors.As(err, &status) || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
	if hits != 0 {
		t.Fatal("redirect followed")
	}
	if _, err := NewHTTPSource(srv.URL, HTTPConfig{Headers: http.Header{"Range": {"bytes=1-"}}}); err == nil {
		t.Fatal("partial header accepted")
	}
}
