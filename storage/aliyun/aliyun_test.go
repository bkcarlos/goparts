package aliyun

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bkcarlos/goparts/storage"
)

func testStore(t *testing.T, handler http.HandlerFunc, modify func(*Config)) (*storage.Client, *Backend) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := Config{Bucket: "test-bucket", Region: "cn-hangzhou", Endpoint: srv.URL, Credentials: Credentials{AccessKeyID: "test-id", AccessKeySecret: "test-secret"}, UsePathStyle: true}
	if modify != nil {
		modify(&cfg)
	}
	backend, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := storage.New(backend, storage.Config{BasePath: "base"})
	if err != nil {
		t.Fatal(err)
	}
	return c, backend
}
func TestAliyunObjectOperationsAndSignedURL(t *testing.T) {
	var mu sync.Mutex
	stored := ""
	store, _ := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") {
			t.Error("OSS v4 signature missing")
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("list-type") == "2" {
			if r.URL.Query().Get("prefix") != "base/a" || r.URL.Query().Get("continuation-token") != "cursor+/=" || r.URL.Query().Get("max-keys") != "2" {
				t.Errorf("query=%v", r.URL.Query())
			}
			io.WriteString(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next+/=</NextContinuationToken><Contents><Key>base/a</Key><Size>5</Size><ETag>etag</ETag></Contents></ListBucketResult>`)
			return
		}
		if r.URL.Path != "/test-bucket/base/a" {
			t.Errorf("path=%s", r.URL.Path)
		}
		switch r.Method {
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			stored = string(data)
			if r.Header.Get("X-Oss-Meta-Team") != "test" {
				t.Error("metadata lost")
			}
			w.Header().Set("ETag", `"etag"`)
		case "GET", "HEAD":
			w.Header().Set("ETag", `"etag"`)
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			if r.Header.Get("Range") != "" {
				if r.Header.Get("Range") != "bytes=1-3" || r.Header.Get("If-Match") != `"etag"` {
					t.Error("range/condition lost")
				}
				w.Header().Set("Content-Range", "bytes 1-3/5")
				w.Header().Set("Content-Length", "3")
				w.WriteHeader(206)
				io.WriteString(w, stored[1:4])
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(stored)))
			if r.Method == "GET" {
				io.WriteString(w, stored)
			}
		case "DELETE":
			stored = ""
			w.WriteHeader(204)
		default:
			t.Errorf("method=%s", r.Method)
		}
	}, nil)
	ctx := context.Background()
	o, err := store.Put(ctx, "a", strings.NewReader("hello"), 5, storage.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"team": "test"}})
	if err != nil || o.ETag != `"etag"` {
		t.Fatalf("put=%+v err=%v", o, err)
	}
	o, err = store.Stat(ctx, "a")
	if err != nil || o.Size != 5 {
		t.Fatalf("stat=%+v err=%v", o, err)
	}
	data, err := store.ReadAll(ctx, "a")
	if err != nil || string(data) != "hello" {
		t.Fatal(err)
	}
	r, err := store.Get(ctx, "a", storage.GetOptions{Offset: 1, Length: 3, IfMatch: o.ETag})
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "ell" || r.Object.Size != 5 || r.Offset != 1 {
		t.Fatalf("range=%s err=%v", data, err)
	}
	page, err := store.List(ctx, storage.ListOptions{Prefix: "a", Cursor: "cursor+/=", Limit: 2})
	if err != nil || page.NextCursor != "next+/=" || len(page.Objects) != 1 || page.Objects[0].Key != "a" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	signed, err := store.PresignGet(ctx, "a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed.URL)
	if err != nil || u.Query().Get("x-oss-signature") == "" || u.Path != "/test-bucket/base/a" || !signed.ExpiresAt.After(time.Now()) {
		t.Fatalf("invalid signed URL, err=%v", err)
	}
	if err = store.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
}

func TestMultipartUploadAndAbort(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var mu sync.Mutex
			parts := map[string]string{}
			completed, aborted := 0, 0
			store, _ := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == "POST" && r.URL.Query().Has("uploads"):
					io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>upload1</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == "PUT":
					data, _ := io.ReadAll(r.Body)
					if fail {
						w.WriteHeader(403)
						io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>private diagnostic</Message><RequestId>req1</RequestId></Error>`)
						return
					}
					parts[r.URL.Query().Get("partNumber")] = string(data)
					w.Header().Set("ETag", `"part-etag"`)
				case r.Method == "POST" && r.URL.Query().Get("uploadId") == "upload1":
					var body struct {
						Parts []struct {
							Number int `xml:"PartNumber"`
						} `xml:"Part"`
					}
					if xml.NewDecoder(r.Body).Decode(&body) != nil || len(body.Parts) != 3 {
						t.Errorf("completion body=%+v", body)
					}
					completed++
					io.WriteString(w, `<CompleteMultipartUploadResult><ETag>final-etag</ETag></CompleteMultipartUploadResult>`)
				case r.Method == "DELETE":
					aborted++
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.String())
					w.WriteHeader(500)
				}
			}, func(c *Config) { c.MultipartThreshold = 100 * 1024; c.PartSize = 100 * 1024; c.Parallelism = 2 })
			data := strings.Repeat("x", 250*1024)
			_, err := store.Put(context.Background(), "large", strings.NewReader(data), int64(len(data)), storage.PutOptions{})
			mu.Lock()
			defer mu.Unlock()
			if fail {
				if !errors.Is(err, storage.ErrPermission) || aborted != 1 || completed != 0 || strings.Contains(err.Error(), "private diagnostic") {
					t.Fatalf("err=%v abort=%d complete=%d", err, aborted, completed)
				}
			} else {
				if err != nil || completed != 1 || aborted != 0 || parts["1"]+parts["2"]+parts["3"] != data {
					t.Fatalf("err=%v complete=%d abort=%d", err, completed, aborted)
				}
			}
		})
	}
}

func TestNotFoundPermissionAndRangeFallback(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   error
	}{{404, "NoSuchKey", storage.ErrNotFound}, {403, "AccessDenied", storage.ErrPermission}, {412, "PreconditionFailed", storage.ErrPrecondition}} {
		store, _ := testStore(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprintf(w, `<Error><Code>%s</Code><RequestId>req</RequestId></Error>`, tc.code)
		}, nil)
		_, err := store.Get(context.Background(), "a", storage.GetOptions{})
		if !errors.Is(err, tc.want) {
			t.Errorf("code=%s err=%v", tc.code, err)
		}
	}
	store, _ := testStore(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ignored range") }, nil)
	if _, err := store.Get(context.Background(), "a", storage.GetOptions{Offset: 1}); !errors.Is(err, storage.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestResponseCapAndCanceledMultipartCleanup(t *testing.T) {
	store, _ := testStore(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 1024)) }, func(c *Config) { c.MaxResponseBytes = 32 })
	if _, err := store.List(context.Background(), storage.ListOptions{}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("metadata cap=%v", err)
	}
	// The same cap must not truncate object content streams.
	if data, err := store.ReadAll(context.Background(), "a"); err != nil || len(data) != 1024 {
		t.Fatalf("stream length=%d err=%v", len(data), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	aborted := false
	store, _ = testStore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>cancel1</UploadId></InitiateMultipartUploadResult>`)
		case "PUT":
			io.Copy(io.Discard, r.Body)
			cancel()
			w.WriteHeader(500)
		case "DELETE":
			mu.Lock()
			aborted = true
			mu.Unlock()
			w.WriteHeader(204)
		}
	}, func(c *Config) {
		c.MultipartThreshold = 100 * 1024
		c.PartSize = 100 * 1024
		c.Parallelism = 1
		c.CleanupTimeout = time.Second
	})
	data := strings.Repeat("x", 150*1024)
	_, err := store.Put(ctx, "a", strings.NewReader(data), int64(len(data)), storage.PutOptions{})
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(err, context.Canceled) || !aborted {
		t.Fatalf("err=%v aborted=%v", err, aborted)
	}
}
