// This example runs entirely against a local mock; no credentials are needed.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/bkcarlos/goparts/feishu/attachment"
	"github.com/bkcarlos/goparts/feishu/card"
	"github.com/bkcarlos/goparts/feishu/user"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume multipart bodies just as a real upload server would.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.Contains(r.URL.Path, "/auth/") {
			io.WriteString(w, `{"code":0,"tenant_access_token":"mock-app-token","expire":7200}`)
			return
		}
		want := "Bearer mock-user-token"
		if strings.Contains(r.URL.Path, "/im/") {
			want = "Bearer mock-app-token"
		}
		if r.Header.Get("Authorization") != want {
			http.Error(w, "wrong identity", 401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /open-apis/im/v1/files":
			io.WriteString(w, `{"code":0,"data":{"file_key":"mock-key"}}`)
		case "POST /open-apis/im/v1/messages":
			io.WriteString(w, `{"code":0,"data":{"message_id":"mock-message","chat_id":"mock-chat"}}`)
		case "POST /open-apis/drive/v1/files/upload_all":
			io.WriteString(w, `{"code":0,"data":{"file_token":"mock-drive-file"}}`)
		case "POST /open-apis/docx/v1/documents/mock-doc/blocks/mock-doc/children":
			io.WriteString(w, `{"code":0,"data":{"children":[{"block_id":"mock-view","block_type":33,"children":["mock-file-block"]}],"document_revision_id":2}}`)
		case "POST /open-apis/drive/v1/medias/upload_all":
			io.WriteString(w, `{"code":0,"data":{"file_token":"mock-media-file"}}`)
		case "PATCH /open-apis/docx/v1/documents/mock-doc/blocks/mock-file-block":
			io.WriteString(w, `{"code":0,"data":{"block":{"block_id":"mock-file-block","block_type":23},"document_revision_id":3}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	base := srv.URL + "/open-apis"
	app, err := card.New(card.Config{AppID: "mock-app", AppSecret: "mock-secret", BaseURL: base})
	if err != nil {
		return err
	}
	chatFiles, err := attachment.New(attachment.Config{TokenProvider: app.AccessToken, BaseURL: base})
	if err != nil {
		return err
	}
	source := func() attachment.Source { return attachment.Source{Reader: strings.NewReader("hello"), Size: 5} }
	opts := attachment.UploadOptions{FileName: "hello.txt"}
	chatFile, err := chatFiles.UploadChat(ctx, source(), opts)
	if err != nil {
		return err
	}
	message, err := chatFiles.SendFile(ctx, attachment.Chat("mock-chat"), chatFile.FileKey, attachment.SendOptions{UUID: "mock-send-1"})
	if err != nil {
		return err
	}
	fmt.Println("模拟聊天附件发送:", message.MessageID)

	// Seed a fake local session. In production use StartLogin/CompleteLogin
	// and the configured TokenStore instead of constructing tokens yourself.
	scopes := []string{user.ScopeWriteDocuments, attachment.ScopeUploadDrive, attachment.ScopeUploadMedia}
	store := &user.MemoryStore{}
	err = store.Save(ctx, user.Token{AppID: "mock-app", Issuer: srv.URL, APIBaseURL: base, OpenID: "mock-user", TokenType: "Bearer", AccessToken: "mock-user-token", Scope: strings.Join(scopes, " ") + " offline_access", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		return err
	}
	docs, err := user.New(user.Config{AppID: "mock-app", AppSecret: "mock-secret", Scopes: scopes, BaseURL: base, AccountsURL: srv.URL, Store: store})
	if err != nil {
		return err
	}
	files, err := attachment.New(attachment.Config{TokenProvider: docs.AccessToken, BaseURL: base})
	if err != nil {
		return err
	}
	driveFile, err := files.UploadDrive(ctx, "mock-folder", source(), opts)
	if err != nil {
		return err
	}
	fmt.Println("模拟用户云空间上传:", driveFile.FileToken)
	block, err := docs.CreateFileBlock(ctx, "mock-doc", "", user.WriteOptions{ClientToken: "mock-create-file"})
	if err != nil {
		return err
	}
	media, err := files.UploadMedia(ctx, attachment.MediaTarget{ParentType: "docx_file", ParentNode: block.BlockID, DocumentToken: "mock-doc"}, source(), opts)
	if err != nil {
		return err
	}
	_, err = docs.ReplaceFile(ctx, "mock-doc", block.BlockID, media.FileToken, user.WriteOptions{ClientToken: "mock-attach-file"})
	if err != nil {
		return err
	}
	fmt.Println("模拟用户文档附件关联:", block.BlockID)
	return nil
}
