package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bkcarlos/goparts/download"
	"github.com/bkcarlos/goparts/storage"
	"github.com/bkcarlos/goparts/storage/aliyun"
)

func TestStorageStreamUsesGenericDownloader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bucket/prefix/file" || r.Header.Get("Authorization") == "" {
			t.Errorf("request path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Length", "5")
		io.WriteString(w, "hello")
	}))
	defer srv.Close()
	backend, err := aliyun.New(aliyun.Config{Bucket: "bucket", Region: "cn-hangzhou", Endpoint: srv.URL, UsePathStyle: true, Credentials: aliyun.Credentials{AccessKeyID: "id", AccessKeySecret: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(backend, storage.Config{BasePath: "prefix"})
	if err != nil {
		t.Fatal(err)
	}
	source := download.SourceFunc(func(ctx context.Context) (download.Stream, error) {
		r, err := store.Get(ctx, "file", storage.GetOptions{})
		if err != nil {
			return download.Stream{}, err
		}
		return download.Stream{Body: r, Size: r.Length}, nil
	})
	client, err := download.New(download.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("hello"))
	path := filepath.Join(t.TempDir(), "file")
	result, err := client.Fetch(context.Background(), source, path, download.Options{SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" || result.Bytes != 5 {
		t.Fatalf("bytes=%d error=%v", result.Bytes, err)
	}
}
