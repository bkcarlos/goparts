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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"你好", "，流式输出正常。"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", part)
			w.(http.Flusher).Flush()
		}
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":6,\"total_tokens\":8}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c, err := llm.New(llm.Config{BaseURL: srv.URL, Model: "mock-chat"})
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseIdleConnections()
	var usage *llm.Usage
	err = c.ChatStream(context.Background(), llm.ChatRequest{Messages: []llm.Message{llm.User("你好")}, IncludeUsage: true}, func(chunk llm.ChatChunk) error {
		for _, choice := range chunk.Choices {
			if choice.Index == 0 {
				fmt.Print(choice.Delta.Content)
			}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	if usage != nil {
		fmt.Printf("tokens: %d\n", usage.TotalTokens)
	}
}
