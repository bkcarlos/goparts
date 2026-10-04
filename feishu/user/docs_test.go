package user

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUserDocumentOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-access" {
			t.Error("document call did not use user access token")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /open-apis/docx/v1/documents":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["title"] != "新文档" || body["folder_token"] != "folder1" {
				t.Errorf("create body=%v", body)
			}
			io.WriteString(w, `{"code":0,"data":{"document":{"document_id":"doc1","revision_id":1,"title":"新文档"}}}`)
		case "GET /open-apis/docx/v1/documents/doc1":
			io.WriteString(w, `{"code":0,"data":{"document":{"document_id":"doc1","revision_id":7,"title":"新文档"}}}`)
		case "GET /open-apis/docx/v1/documents/doc1/raw_content":
			io.WriteString(w, `{"code":0,"data":{"content":"文档内容"}}`)
		case "GET /open-apis/docx/v1/documents/doc1/blocks":
			if r.URL.Query().Get("document_revision_id") != "7" || r.URL.Query().Get("page_size") != "1" {
				t.Error("pagination lost snapshot")
			}
			if r.URL.Query().Get("page_token") == "" {
				io.WriteString(w, `{"code":0,"data":{"items":[{"block_id":"b1","block_type":2,"text":{"elements":[]}}],"has_more":true,"page_token":"next+/="}}`)
			} else {
				if r.URL.Query().Get("page_token") != "next+/=" {
					t.Error("page token corrupted")
				}
				io.WriteString(w, `{"code":0,"data":{"items":[{"block_id":"b2","block_type":3,"heading1":{"elements":[]}}],"has_more":false}}`)
			}
		case "POST /open-apis/docx/v1/documents/doc1/blocks/doc1/children":
			var body struct {
				Index    int     `json:"index"`
				Children []Block `json:"children"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Index != -1 || len(body.Children) != 1 || body.Children[0]["block_type"] != float64(2) || r.URL.Query().Get("client_token") != "idempotency-key" {
				t.Errorf("append body=%v query=%v", body, r.URL.Query())
			}
			io.WriteString(w, `{"code":0,"data":{"children":[{"block_id":"new-block","block_type":2}],"document_revision_id":8,"client_token":"idempotency-key"}}`)
		case "PATCH /open-apis/docx/v1/documents/doc1/blocks/b1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["update_text_elements"] == nil || r.URL.Query().Get("document_revision_id") != "7" {
				t.Error("invalid update payload")
			}
			io.WriteString(w, `{"code":0,"data":{"block":{"block_id":"b1","block_type":2},"document_revision_id":9}}`)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	save(t, c, activeToken(c))
	ctx := context.Background()
	doc, err := c.CreateDocument(ctx, "新文档", "folder1")
	if err != nil || doc.ID != "doc1" {
		t.Fatalf("create: %+v %v", doc, err)
	}
	doc, err = c.GetDocument(ctx, doc.ID)
	if err != nil || doc.RevisionID != 7 {
		t.Fatal(err)
	}
	text, err := c.ReadDocument(ctx, doc.ID)
	if err != nil || text != "文档内容" {
		t.Fatalf("read: %q %v", text, err)
	}
	opts := PageOptions{PageSize: 1, RevisionID: &doc.RevisionID}
	page, err := c.ListBlocks(ctx, doc.ID, opts)
	if err != nil || !page.HasMore || page.Items[0].ID() != "b1" {
		t.Fatal(err)
	}
	opts.PageToken = page.PageToken
	page, err = c.ListBlocks(ctx, doc.ID, opts)
	if err != nil || page.HasMore || page.Items[0]["heading1"] == nil {
		t.Fatal(err)
	}
	appendResult, err := c.AppendText(ctx, doc.ID, "追加内容", WriteOptions{ClientToken: "idempotency-key"})
	if err != nil || appendResult.Children[0].ID() != "new-block" {
		t.Fatal(err)
	}
	updated, err := c.UpdateText(ctx, doc.ID, "b1", "新内容", WriteOptions{RevisionID: &doc.RevisionID})
	if err != nil || updated.RevisionID != 9 {
		t.Fatal(err)
	}
}

func TestDocumentErrorsNeverRetryOrFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"permission", 403, `{"code":1770032,"msg":"sensitive document title"}`},
		{"business", 200, `{"code":1770001,"msg":"invalid"}`},
		{"expired user token", 401, `{"code":99991668,"msg":"expired"}`},
		{"rate limit", 429, `limited`},
		{"missing code", 200, `{"data":{}}`},
		{"missing data", 200, `{"code":0}`},
		{"null data", 200, `{"code":0,"data":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("X-Tt-Logid", "trace")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c := mustClient(t, testConfig(srv.URL))
			save(t, c, activeToken(c))
			_, err := c.AppendText(context.Background(), "doc1", "text", WriteOptions{})
			if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), "sensitive document title") {
				t.Fatalf("calls=%d error=%v", calls.Load(), err)
			}
		})
	}
}

func TestValidationBeforeNetwork(t *testing.T) {
	c := mustClient(t, testConfig("https://example.com"))
	ctx := context.Background()
	badRevision := int64(-2)
	checks := []func() error{
		func() error { _, e := c.ReadDocument(ctx, "../bad"); return e },
		func() error { _, e := c.GetDocument(ctx, "https://example.com/docx/id"); return e },
		func() error { _, e := c.CreateDocument(ctx, strings.Repeat("中", 801), ""); return e },
		func() error { _, e := c.CreateDocument(ctx, "title", "../folder"); return e },
		func() error { _, e := c.ListBlocks(ctx, "doc", PageOptions{PageSize: 501}); return e },
		func() error { _, e := c.ListBlocks(ctx, "doc", PageOptions{RevisionID: &badRevision}); return e },
		func() error { _, e := c.AppendBlocks(ctx, "doc", "", nil, WriteOptions{}); return e },
		func() error { _, e := c.AppendBlocks(ctx, "doc", "", make([]Block, 51), WriteOptions{}); return e },
		func() error {
			_, e := c.AppendBlocks(ctx, "doc", "../bad", []Block{Paragraph("text")}, WriteOptions{})
			return e
		},
		func() error { _, e := c.UpdateText(ctx, "doc", "", "text", WriteOptions{}); return e },
	}
	for i, check := range checks {
		if err := check(); err == nil || errors.Is(err, ErrLoginRequired) {
			t.Errorf("case %d failed local validation: %v", i, err)
		}
	}
	for _, cfg := range []Config{{}, {AppID: "app", AppSecret: "secret"}, {AppID: "app", AppSecret: "secret", Scopes: []string{"x"}, AccountsURL: "https://u:p@example.com"}, {AppID: "app", AppSecret: "secret", Scopes: []string{"x"}, Timeout: -1}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}

func TestRedirectAndTimeout(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer srv.Close()
	custom := &http.Client{}
	cfg := testConfig(srv.URL)
	cfg.HTTPClient = custom
	c := mustClient(t, cfg)
	save(t, c, activeToken(c))
	_, err := c.ReadDocument(context.Background(), "doc1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 307 || forwarded.Load() != 0 || custom.CheckRedirect != nil {
		t.Fatal("redirect behavior invalid")
	}
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer blocked.Close()
	cfg = testConfig(blocked.URL)
	cfg.Timeout = 10 * time.Millisecond
	c = mustClient(t, cfg)
	save(t, c, activeToken(c))
	if _, err := c.ReadDocument(context.Background(), "doc1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
