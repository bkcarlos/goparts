// This example sends a real request to the configured provider.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bkcarlos/goparts/llm"
)

func main() {
	stream := flag.Bool("stream", false, "stream the response")
	prompt := flag.String("prompt", "你好，请简短介绍一下自己。", "prompt to send")
	flag.Parse()
	if os.Getenv("LLM_MODEL") == "" {
		log.Fatal("请设置 LLM_MODEL；LLM_BASE_URL 可设置兼容接口根地址，LLM_API_KEY 设置对应密钥")
	}
	c, err := llm.New(llm.Config{BaseURL: os.Getenv("LLM_BASE_URL"), APIKey: os.Getenv("LLM_API_KEY"), Model: os.Getenv("LLM_MODEL")})
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseIdleConnections()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	input := llm.ChatRequest{Messages: []llm.Message{llm.User(*prompt)}}
	if *stream {
		err = c.ChatStream(ctx, input, func(chunk llm.ChatChunk) error {
			for _, choice := range chunk.Choices {
				if choice.Index == 0 {
					fmt.Print(choice.Delta.Content)
				}
			}
			return nil
		})
		fmt.Println()
	} else {
		var response *llm.ChatResponse
		response, err = c.Chat(ctx, input)
		if err == nil {
			fmt.Println(response.Text())
		}
	}
	if err != nil {
		log.Fatal(err)
	}
}
