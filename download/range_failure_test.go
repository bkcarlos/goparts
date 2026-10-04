package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestResumeRedownloadsTamperedChunk(t *testing.T) {
	content := "abcdefghijkl"
	var mu sync.Mutex
	fail := true
	counts := map[int]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		counts[start]++
		shouldFail := fail && start == 4
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(503)
			return
		}
		if start < 0 || end >= len(content) || start > end {
			w.WriteHeader(416)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.WriteHeader(206)
		io.WriteString(w, content[start:end+1])
	}))
	defer srv.Close()
	source, err := NewHTTPSource(srv.URL, HTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "file")
	opts := RangeOptions{Mode: Parallel, Workers: 1, ChunkBytes: 4, Resume: true}
	if _, err := c.FetchRanges(context.Background(), source, dst, opts); err == nil {
		t.Fatal("interrupted download succeeded")
	}
	chunk := filepath.Join(dst+".goparts-part", "chunk-000000")
	if got, err := os.ReadFile(chunk); err != nil || string(got) != "abcd" {
		t.Fatalf("missing completed chunk: %q %v", got, err)
	}
	// Same length corruption must be detected by the persisted chunk digest.
	if err := os.WriteFile(chunk, []byte("XXXX"), 0600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	fail = false
	before := counts[0]
	mu.Unlock()
	if _, err := c.FetchRanges(context.Background(), source, dst, opts); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	after := counts[0]
	mu.Unlock()
	if after-before != 2 {
		t.Fatalf("expected probe and replacement chunk, got %d requests", after-before)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != content {
		t.Fatalf("published corruption: %q %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("resume files leaked: %v %v", entries, err)
	}
}

func TestHTTPRangeRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name, span, etag, encoding, body string
		status                           int
	}{
		{"ignored range", "", `"v1"`, "", "abc", 200},
		{"wrong offset", "bytes 1-3/4", `"v1"`, "", "abc", 206},
		{"wrong end", "bytes 0-1/4", `"v1"`, "", "abc", 206},
		{"invalid total", "bytes 0-2/2", `"v1"`, "", "abc", 206},
		{"malformed", "nonsense", `"v1"`, "", "abc", 206},
		{"wrong length", "bytes 0-2/4", `"v1"`, "", "ab", 206},
		{"changed version", "bytes 0-2/4", `"v2"`, "", "abc", 206},
		{"compressed", "bytes 0-2/4", `"v1"`, "br", "abc", 206},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=0-2" || r.Header.Get("If-Match") != `"v1"` {
					t.Errorf("missing conditional range headers: %v", r.Header)
				}
				w.Header().Set("Content-Range", tc.span)
				w.Header().Set("ETag", tc.etag)
				w.Header().Set("Content-Encoding", tc.encoding)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			source, err := NewHTTPSource(srv.URL, HTTPConfig{})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := source.OpenRange(context.Background(), 0, 3, `"v1"`)
			if stream.Body != nil {
				stream.Body.Close()
			}
			if err == nil {
				t.Fatal("invalid range accepted")
			}
		})
	}
}

type brokenRangeSource struct {
	body     string
	closeErr error
	closed   *bool
}

func (s brokenRangeSource) Open(context.Context) (Stream, error) {
	return Stream{}, errors.New("unexpected sequential download")
}
func (s brokenRangeSource) Probe(context.Context) (RangeInfo, error) {
	return RangeInfo{Size: 4, Supported: true, ETag: `"v1"`, Identity: "object"}, nil
}
func (s brokenRangeSource) OpenRange(context.Context, int64, int64, string) (Stream, error) {
	return Stream{Body: &rangeBody{Reader: strings.NewReader(s.body), closed: s.closed, err: s.closeErr}, Size: 4}, nil
}

type rangeBody struct {
	io.Reader
	closed *bool
	err    error
}

func (r *rangeBody) Close() error { *r.closed = true; return r.err }

func TestRangeFailurePreservesDestinationAndCleansTemps(t *testing.T) {
	closeErr := errors.New("close failed")
	for _, tc := range []struct {
		name, body     string
		closeErr, want error
	}{
		{"short body", "abc", nil, ErrSizeMismatch},
		{"long body", "abcde", nil, ErrSizeMismatch},
		{"close error", "abcd", closeErr, closeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dst := filepath.Join(dir, "file")
			if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := New(Config{})
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			_, err = c.FetchRanges(context.Background(), brokenRangeSource{tc.body, tc.closeErr, &closed}, dst, RangeOptions{Options: Options{Overwrite: true}, Mode: Parallel, Workers: 1})
			if !errors.Is(err, tc.want) || !closed {
				t.Fatalf("err=%v body closed=%v", err, closed)
			}
			got, err := os.ReadFile(dst)
			if err != nil || string(got) != "old" {
				t.Fatalf("old file lost: %q %v", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("partial files leaked: %v %v", entries, err)
			}
		})
	}
}

func FuzzParseContentRange(f *testing.F) {
	for _, s := range []string{"bytes 0-0/1", "bytes 4-7/12", "bytes */0", "bytes -1-0/1", "bytes 0-2/2", "bytes 0-1/9223372036854775808"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		start, end, total, err := parseRange(text)
		if err == nil && (start < 0 || end < start || total <= end) {
			t.Fatalf("accepted invalid range %q: %d %d %d", text, start, end, total)
		}
	})
}
