package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/bkcarlos/goparts/storage"
	"github.com/bkcarlos/goparts/storage/aliyun"
)

func main() {
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			body = string(data)
			w.Header().Set("ETag", `"mock-etag"`)
		case "GET":
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			io.WriteString(w, body)
		default:
			http.Error(w, "unsupported", 400)
		}
	}))
	defer srv.Close()
	backend, err := aliyun.New(aliyun.Config{Bucket: "mock-bucket", Region: "cn-hangzhou", Endpoint: srv.URL, UsePathStyle: true, Credentials: aliyun.Credentials{AccessKeyID: "mock-id", AccessKeySecret: "mock-secret"}})
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.New(backend, storage.Config{BasePath: "my-app"})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	data := "hello storage"
	object, err := store.Put(ctx, "example.txt", strings.NewReader(data), int64(len(data)), storage.PutOptions{})
	if err != nil {
		log.Fatal(err)
	}
	read, err := store.ReadAll(ctx, object.Key)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("本地 OSS 模拟上传/读取：%s，%d 字节\n", object.Key, len(read))
}
