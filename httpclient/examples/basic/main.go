package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/bkcarlos/goparts/httpclient"
)

func main() {
	// Use a local server so this example can run without an external service.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"message":"hello"}`)
	}))
	defer srv.Close()
	client, err := httpclient.New(httpclient.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer client.CloseIdleConnections()
	var result struct {
		Message string `json:"message"`
	}
	if _, err := client.DoJSON(context.Background(), http.MethodGet, srv.URL, nil, &result); err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message)
}
