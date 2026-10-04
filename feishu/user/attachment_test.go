package user

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentFileBlocks(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer user-access" || r.URL.Query().Get("document_revision_id") != "-1" {
			t.Error("missing user identity or revision")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /open-apis/docx/v1/documents/doc1/blocks/doc1/children":
			children, _ := body["children"].([]any)
			if len(children) != 1 {
				t.Error("expected one file block")
				return
			}
			child, _ := children[0].(map[string]any)
			file, _ := child["file"].(map[string]any)
			if child["block_type"] != float64(23) || file["token"] != "" || body["index"] != float64(-1) || r.URL.Query().Get("client_token") != "create-file" {
				t.Errorf("body=%v query=%v", body, r.URL.Query())
			}
			io.WriteString(w, `{"code":0,"data":{"children":[{"block_id":"view1","block_type":33,"children":["file1"]}],"document_revision_id":8}}`)
		case "PATCH /open-apis/docx/v1/documents/doc1/blocks/file1":
			file, _ := body["replace_file"].(map[string]any)
			if file["token"] != "media1" || r.URL.Query().Get("client_token") != "attach-file" {
				t.Errorf("body=%v", body)
			}
			io.WriteString(w, `{"code":0,"data":{"block":{"block_id":"file1","block_type":23,"file":{"token":"media1"}},"document_revision_id":9}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	save(t, c, activeToken(c))
	ctx := context.Background()
	block, err := c.CreateFileBlock(ctx, "doc1", "", WriteOptions{ClientToken: "create-file"})
	if err != nil || block.BlockID != "file1" || block.ViewBlockID != "view1" || block.RevisionID != 8 {
		t.Fatalf("block=%+v err=%v", block, err)
	}
	updated, err := c.ReplaceFile(ctx, "doc1", block.BlockID, "media1", WriteOptions{ClientToken: "attach-file"})
	if err != nil || updated.Block.ID() != "file1" || updated.RevisionID != 9 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	for _, args := range [][3]string{{"", "file1", "media1"}, {"doc1", "../bad", "media1"}, {"doc1", "file1", ""}} {
		if _, err := c.ReplaceFile(ctx, args[0], args[1], args[2], WriteOptions{}); err == nil {
			t.Error("accepted invalid ID")
		}
	}
	if calls != 2 {
		t.Fatalf("unexpected calls=%d", calls)
	}
}

func TestCreateFileBlockInvalidResponsePreservesView(t *testing.T) {
	for _, children := range []string{`[]`, `["a","b"]`, `[42]`, `["../bad"]`, `null`} {
		t.Run(children, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"code":0,"data":{"children":[{"block_id":"view1","block_type":33,"children":`+children+`}],"document_revision_id":8}}`)
			}))
			defer srv.Close()
			c := mustClient(t, testConfig(srv.URL))
			save(t, c, activeToken(c))
			block, err := c.CreateFileBlock(context.Background(), "doc1", "", WriteOptions{})
			if !errors.Is(err, ErrInvalidResponse) || block.ViewBlockID != "view1" {
				t.Fatalf("block=%+v err=%v", block, err)
			}
		})
	}
}
