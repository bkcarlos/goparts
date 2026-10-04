package wiki_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bkcarlos/goparts/feishu/wiki"
)

func TestResolveUsesConfiguredHostAndValidatesNodeType(t *testing.T) {
	for _, tc := range []struct {
		name, link, kind, object string
		wantErr                  bool
		requests                 int32
	}{
		{"token", "node", "bitable", "app", false, 1},
		{"foreign link host", "https://untrusted.invalid/wiki/node?secret=x", "bitable", "app", false, 1},
		{"wrong object type", "node", "docx", "doc", true, 1},
		{"missing object token", "node", "bitable", "", true, 1},
		{"http link", "http://example.com/wiki/node", "bitable", "app", true, 0},
		{"credentials in URL", "https://user:pass@example.com/wiki/node", "bitable", "app", true, 0},
		{"wrong path", "https://example.com/docx/node", "bitable", "app", true, 0},
		{"extra segment", "https://example.com/wiki/node/extra", "bitable", "app", true, 0},
		{"encoded separator", "https://example.com/wiki/a%2Fb", "bitable", "app", true, 0},
		{"traversal", "../node", "bitable", "app", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/wiki/v2/spaces/get_node" || r.URL.Query().Get("token") != "node" || r.Header.Get("Authorization") != "Bearer user" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"node": map[string]string{"obj_type": tc.kind, "obj_token": tc.object}}})
			}))
			defer srv.Close()
			c, err := wiki.New(wiki.Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "user", nil }})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ResolveBitable(context.Background(), tc.link)
			if (err != nil) != tc.wantErr || !tc.wantErr && got != tc.object {
				t.Fatalf("result=%q err=%v", got, err)
			}
			if calls.Load() != tc.requests {
				t.Fatalf("requests=%d want=%d", calls.Load(), tc.requests)
			}
		})
	}
}
