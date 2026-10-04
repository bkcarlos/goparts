package bitable_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/bkcarlos/goparts/feishu/bitable"
)

func TestTableRecordFieldWireContracts(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, method, path, query, body, response string
		call                                      func(*bitable.Client) (any, error)
		want                                      any
	}{
		{"list tables", "GET", "/tables", "page_size=2&page_token=next&user_id_type=union_id", "", `{"items":[{"table_id":"tbl","name":"name"}],"has_more":true,"page_token":"more","total":3}`, func(c *bitable.Client) (any, error) {
			return c.ListTables(ctx, "app", bitable.Page{PageSize: 2, PageToken: "next", UserIDType: "union_id"})
		}, bitable.PageResult[bitable.Table]{Items: []bitable.Table{{TableID: "tbl", Name: "name"}}, HasMore: true, PageToken: "more", Total: 3}},
		{"create table", "POST", "/tables", "", `{"table":{"name":"表格"}}`, `{"table_id":"tbl","default_view_id":"view"}`, func(c *bitable.Client) (any, error) { return c.CreateTable(ctx, "app", "表格") }, bitable.Table{TableID: "tbl", Name: "表格", DefaultViewID: "view"}},
		{"rename table", "PATCH", "/tables/tbl", "", `{"name":"new"}`, `{}`, func(c *bitable.Client) (any, error) { return nil, c.UpdateTable(ctx, "app", "tbl", "new") }, nil},
		{"delete table", "DELETE", "/tables/tbl", "", "", `{}`, func(c *bitable.Client) (any, error) { return nil, c.DeleteTable(ctx, "app", "tbl") }, nil},
		{"create records", "POST", "/tables/tbl/records/batch_create", "", `{"records":[{"fields":{"title":"hello"}}]}`, `{"records":[{"record_id":"rec","fields":{"title":"hello"}}]}`, func(c *bitable.Client) (any, error) {
			return c.BatchCreate(ctx, "app", "tbl", []bitable.Record{{Fields: map[string]any{"title": "hello"}}}, bitable.Page{})
		}, []bitable.Record{{RecordID: "rec", Fields: map[string]any{"title": "hello"}}}},
		{"update records", "POST", "/tables/tbl/records/batch_update", "", `{"records":[{"record_id":"rec","fields":{"title":"new"}}]}`, `{"records":[{"record_id":"rec","fields":{"title":"new"}}]}`, func(c *bitable.Client) (any, error) {
			return c.BatchUpdate(ctx, "app", "tbl", []bitable.Record{{RecordID: "rec", Fields: map[string]any{"title": "new"}}}, bitable.Page{})
		}, []bitable.Record{{RecordID: "rec", Fields: map[string]any{"title": "new"}}}},
		{"search", "POST", "/tables/tbl/records/search", "page_token=next", `{"view_id":"view","field_names":["title"],"sort":[{"field_name":"title","desc":true}]}`, `{"items":[],"has_more":false}`, func(c *bitable.Client) (any, error) {
			return c.Search(ctx, "app", "tbl", bitable.SearchRequest{ViewID: "view", FieldNames: []string{"title"}, Sort: []bitable.Sort{{FieldName: "title", Desc: true}}}, bitable.Page{PageToken: "next"})
		}, bitable.PageResult[bitable.Record]{Items: []bitable.Record{}}},
		{"list fields", "GET", "/tables/tbl/fields", "", "", `{"items":[{"field_id":"fld","field_name":"title","type":1}]}`, func(c *bitable.Client) (any, error) { return c.ListFields(ctx, "app", "tbl", bitable.Page{}) }, bitable.PageResult[bitable.Field]{Items: []bitable.Field{{FieldID: "fld", FieldName: "title", Type: 1}}}},
		{"create field", "POST", "/tables/tbl/fields", "", `{"field_name":"title","type":1}`, `{"field":{"field_id":"fld","field_name":"title","type":1}}`, func(c *bitable.Client) (any, error) {
			return c.CreateField(ctx, "app", "tbl", bitable.Field{FieldID: "ignored", FieldName: "title", Type: 1})
		}, bitable.Field{FieldID: "fld", FieldName: "title", Type: 1}},
		{"update field", "PUT", "/tables/tbl/fields/fld", "", `{"field_name":"new","type":1}`, `{"field":{"field_id":"fld","field_name":"new","type":1}}`, func(c *bitable.Client) (any, error) {
			return c.UpdateField(ctx, "app", "tbl", "fld", bitable.Field{FieldName: "new", Type: 1})
		}, bitable.Field{FieldID: "fld", FieldName: "new", Type: 1}},
		{"delete field", "DELETE", "/tables/tbl/fields/fld", "", "", `{}`, func(c *bitable.Client) (any, error) { return nil, c.DeleteField(ctx, "app", "tbl", "fld") }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != tc.method || r.URL.Path != "/bitable/v1/apps/app"+tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer identity" {
					t.Error("identity missing")
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if tc.body == "" {
					if len(data) > 0 {
						t.Errorf("unexpected body: %s", data)
					}
				} else {
					var got, want any
					if err := json.Unmarshal(data, &got); err != nil {
						t.Error(err)
					}
					if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
						t.Error(err)
						return
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body=%s want=%s", data, tc.body)
					}
				}
				io.WriteString(w, `{"code":0,"data":`+tc.response+`}`)
			}))
			defer srv.Close()
			c, err := bitable.New(bitable.Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "identity", nil }})
			if err != nil {
				t.Fatal(err)
			}
			got, err := tc.call(c)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result=%#v want=%#v err=%v", got, tc.want, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests=%d", calls.Load())
			}
		})
	}
}

func TestInvalidInputNeverReachesNetwork(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	c, err := bitable.New(bitable.Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "identity", nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cases := map[string]func() error{
		"traversal":     func() error { _, err := c.ListTables(ctx, "../secret", bitable.Page{}); return err },
		"negative page": func() error { _, err := c.ListTables(ctx, "app", bitable.Page{PageSize: -1}); return err },
		"large page":    func() error { _, err := c.ListTables(ctx, "app", bitable.Page{PageSize: 501}); return err },
		"id type":       func() error { _, err := c.ListTables(ctx, "app", bitable.Page{UserIDType: "bad"}); return err },
		"empty table":   func() error { _, err := c.CreateTable(ctx, "app", " "); return err },
		"empty rename":  func() error { return c.UpdateTable(ctx, "app", "tbl", "") },
		"empty batch":   func() error { _, err := c.BatchCreate(ctx, "app", "tbl", nil, bitable.Page{}); return err },
		"large batch": func() error {
			_, err := c.BatchCreate(ctx, "app", "tbl", make([]bitable.Record, 501), bitable.Page{})
			return err
		},
		"empty fields": func() error {
			_, err := c.BatchCreate(ctx, "app", "tbl", []bitable.Record{{}}, bitable.Page{})
			return err
		},
		"missing record id": func() error {
			_, err := c.BatchUpdate(ctx, "app", "tbl", []bitable.Record{{Fields: map[string]any{"a": 1}}}, bitable.Page{})
			return err
		},
		"missing field name": func() error { _, err := c.CreateField(ctx, "app", "tbl", bitable.Field{Type: 1}); return err },
		"missing field type": func() error { _, err := c.CreateField(ctx, "app", "tbl", bitable.Field{FieldName: "name"}); return err },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			if err := fn(); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("sent %d invalid requests", calls.Load())
	}
}
