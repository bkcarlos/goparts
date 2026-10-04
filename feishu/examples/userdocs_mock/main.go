package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/bkcarlos/goparts/feishu/user"
)

func main() {
	// Simulates user confirmation locally; no real Feishu app or credentials.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/v1/device_authorization":
			io.WriteString(w, `{"device_code":"mock-device","user_code":"MOCK","verification_uri":"https://example.com/mock-login","expires_in":60,"interval":1}`)
		case "/oauth/v3/token":
			io.WriteString(w, `{"access_token":"mock-user-token","refresh_token":"mock-refresh","expires_in":7200,"refresh_token_expires_in":86400,"scope":"docx:document offline_access","token_type":"Bearer"}`)
		case "/open-apis/authen/v1/user_info":
			io.WriteString(w, `{"code":0,"data":{"open_id":"mock-user","name":"本地示例用户"}}`)
		case "/open-apis/docx/v1/documents":
			io.WriteString(w, `{"code":0,"data":{"document":{"document_id":"mock-doc","title":"示例文档","revision_id":1}}}`)
		case "/open-apis/docx/v1/documents/mock-doc/blocks/mock-doc/children":
			io.WriteString(w, `{"code":0,"data":{"children":[{"block_id":"mock-block","block_type":2}],"document_revision_id":2}}`)
		case "/open-apis/docx/v1/documents/mock-doc/raw_content":
			io.WriteString(w, `{"code":0,"data":{"content":"以用户身份写入的示例内容"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := user.New(user.Config{AppID: "mock-app", AppSecret: "mock-secret", Scopes: []string{user.ScopeWriteDocuments}, BaseURL: srv.URL + "/open-apis", AccountsURL: srv.URL})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	auth, err := c.StartLogin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	identity, err := c.CompleteLogin(ctx, auth)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("模拟登录: %s\n", identity.Name)
	doc, err := c.CreateDocument(ctx, "示例文档", "")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := c.AppendText(ctx, doc.ID, "以用户身份写入的示例内容", user.WriteOptions{}); err != nil {
		log.Fatal(err)
	}
	content, err := c.ReadDocument(ctx, doc.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(content)
}
