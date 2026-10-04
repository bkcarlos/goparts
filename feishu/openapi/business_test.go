package openapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bkcarlos/goparts/feishu/bitable"
	"github.com/bkcarlos/goparts/feishu/contact"
	"github.com/bkcarlos/goparts/feishu/openapi"
	"github.com/bkcarlos/goparts/feishu/wiki"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBusinessEndpointsPaginationAndIdentity(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("wrong identity")
		}
		var data any
		switch r.URL.Path {
		case "/bitable/v1/apps/app/tables/table/records/search":
			if r.Method != "POST" || r.URL.Query().Get("page_token") != "next" {
				t.Error("query")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["view_id"] != "view" {
				t.Error(body)
			}
			data = map[string]any{"items": []any{map[string]any{"record_id": "r1", "fields": map[string]any{"title": "value"}}}, "has_more": true, "page_token": "more"}
		case "/wiki/v2/spaces/get_node":
			if r.URL.Query().Get("token") != "node" {
				t.Error("token")
			}
			data = map[string]any{"node": map[string]string{"obj_type": "bitable", "obj_token": "app"}}
		case "/contact/v3/users/batch_get_id":
			if r.URL.Query().Get("user_id_type") != "open_id" {
				t.Error("ID kind")
			}
			data = map[string]any{"user_list": []any{map[string]string{"user_id": "ou_1", "email": "a@example.com"}}}
		default:
			t.Error(r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer srv.Close()
	cfg := openapi.Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "user-token", nil }}
	b, _ := bitable.New(cfg)
	records, err := b.Search(context.Background(), "app", "table", bitable.SearchRequest{ViewID: "view"}, bitable.Page{PageToken: "next", PageSize: 10})
	if err != nil || !records.HasMore || records.Items[0].RecordID != "r1" {
		t.Fatal(records, err)
	}
	wc, _ := wiki.New(cfg)
	token, err := wc.ResolveBitable(context.Background(), "https://example.feishu.cn/wiki/node")
	if err != nil || token != "app" {
		t.Fatal(token, err)
	}
	cc, _ := contact.New(cfg)
	ids, err := cc.BatchGetID(context.Background(), contact.BatchRequest{Emails: []string{"a@example.com"}})
	if err != nil || ids[0].UserID != "ou_1" {
		t.Fatal(ids, err)
	}
	if _, err = b.ListTables(context.Background(), "../bad", bitable.Page{}); err == nil || calls != 3 {
		t.Fatal("unsafe path")
	}
}
func TestProviderFailureDoesNotFallback(t *testing.T) {
	failure := errors.New("expired")
	c, _ := openapi.New(openapi.Config{TokenProvider: func(context.Context) (string, error) { return "", failure }})
	if err := c.Do(context.Background(), "GET", "/example", nil, nil, nil); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
