package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/bkcarlos/goparts/llm"
)

func main() {
	// Local mock: this example requires neither an API key nor a paid model.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"你好，LLM 模块已就绪。"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`)
		case "/v1/embeddings":
			io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := llm.New(llm.Config{BaseURL: srv.URL + "/v1", Model: "mock-chat"})
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseIdleConnections()
	ctx := context.Background()
	response, err := c.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{llm.System("请简短回答"), llm.User("你好")}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(response.Text())
	vectors, err := c.Embeddings(ctx, llm.EmbeddingRequest{Model: "mock-embedding", Input: []string{"示例文本"}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("embedding: %v\n", vectors.Data[0].Embedding)
}
