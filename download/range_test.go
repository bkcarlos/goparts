package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRangeResumeAndVersionBinding(t *testing.T) {
	content := []byte(strings.Repeat("abcdefgh", 1024))
	var mu sync.Mutex
	fail := true
	etag := "\"v1\""
	counts := map[int]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			w.Write(content)
			return
		}
		mu.Lock()
		version := etag
		shouldFail := fail && start == 2048
		counts[start]++
		mu.Unlock()
		if match := r.Header.Get("If-Match"); match != "" && match != version {
			w.WriteHeader(412)
			return
		}
		if shouldFail {
			w.WriteHeader(503)
			return
		}
		if end >= len(content) {
			end = len(content) - 1
		}
		w.Header().Set("ETag", version)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.WriteHeader(206)
		w.Write(content[start : end+1])
	}))
	defer srv.Close()
	source, _ := NewHTTPSource(srv.URL, HTTPConfig{})
	client, _ := New(Config{})
	dst := filepath.Join(t.TempDir(), "file")
	sum := sha256.Sum256(content)
	opts := RangeOptions{Options: Options{SHA256: hex.EncodeToString(sum[:])}, Workers: 1, ChunkBytes: 1024, Resume: true, Mode: Parallel}
	if _, err := client.FetchRanges(context.Background(), source, dst, opts); err == nil {
		t.Fatal("expected interrupted range")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("published partial")
	}
	mu.Lock()
	fail = false
	etag = "\"v2\""
	mu.Unlock()
	if _, err := client.FetchRanges(context.Background(), source, dst, opts); err == nil {
		t.Fatal("mixed versions")
	}
	mu.Lock()
	etag = "\"v1\""
	before := counts[1024]
	mu.Unlock()
	result, err := client.FetchRanges(context.Background(), source, dst, opts)
	if err != nil || result.Bytes != int64(len(content)) {
		t.Fatal(result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts[1024] != before {
		t.Fatal("completed chunk downloaded again")
	}
	b, _ := os.ReadFile(dst)
	if string(b) != string(content) {
		t.Fatal("corruption")
	}
	if _, err = os.Stat(dst + ".goparts-part"); !os.IsNotExist(err) {
		t.Fatal("left resume state")
	}
}
func TestRangeAutoFallbackAndBadRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "whole") }))
	defer srv.Close()
	s, _ := NewHTTPSource(srv.URL, HTTPConfig{})
	c, _ := New(Config{})
	result, err := c.FetchRanges(context.Background(), s, filepath.Join(t.TempDir(), "file"), RangeOptions{})
	if err != nil || result.Bytes != 5 {
		t.Fatal(result, err)
	}
	if _, err = c.FetchRanges(context.Background(), s, filepath.Join(t.TempDir(), "file"), RangeOptions{Mode: Parallel}); err == nil {
		t.Fatal("range unsupported")
	}
}
