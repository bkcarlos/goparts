package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/bkcarlos/goparts/download"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "local download example\n") }))
	defer srv.Close()
	dir, err := os.MkdirTemp("", "goparts-download-example-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source, err := download.NewHTTPSource(srv.URL, download.HTTPConfig{})
	if err != nil {
		log.Fatal(err)
	}
	client, err := download.New(download.Config{})
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.Fetch(context.Background(), source, filepath.Join(dir, "example.txt"), download.Options{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("本地模拟下载完成：%d 字节，SHA-256=%s\n", result.Bytes, result.SHA256)
}
