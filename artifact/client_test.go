package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogAndVerifiedDownload(t *testing.T) {
	sum := sha256.Sum256([]byte("binary"))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file" {
			if r.Header.Get("Authorization") != "" {
				t.Error("catalog credential leaked")
			}
			w.Write([]byte("binary"))
			return
		}
		p := AppPackage{ID: "id", URL: srv.URL + "/file", SHA256: hex.EncodeToString(sum[:]), Size: 6}
		if r.URL.Path == "/packages/id" {
			json.NewEncoder(w).Encode(p)
		} else {
			if r.URL.Query().Get("commit_id") != "commit" {
				t.Error("query")
			}
			json.NewEncoder(w).Encode(Page{Items: []AppPackage{p}})
		}
	}))
	defer srv.Close()
	backend, _ := NewHTTPBackend(Config{BaseURL: srv.URL, Headers: http.Header{"Authorization": {"Bearer secret"}}})
	m := &PackageManager{Backend: backend}
	p, err := m.GetPackageInfo(context.Background(), "id")
	if err != nil {
		t.Fatal(err)
	}
	if page, err := m.QueryWithCommitID(context.Background(), "commit", "", 10); err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	dst := filepath.Join(t.TempDir(), "file")
	if err = m.DownloadPackage(context.Background(), p, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "binary" {
		t.Fatal(string(b))
	}
	if err = m.DownloadPackage(context.Background(), p, dst); err == nil {
		t.Fatal("overwrite")
	}
}
