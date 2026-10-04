package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bkcarlos/goparts/feishu/card"
	"github.com/bkcarlos/goparts/feishu/events"
)

func main() {
	appID, appSecret := os.Getenv("FEISHU_APP_ID"), os.Getenv("FEISHU_APP_SECRET")
	if appID == "" || appSecret == "" {
		log.Println("跳过真实长连接：请设置 FEISHU_APP_ID / FEISHU_APP_SECRET")
		return
	}
	dispatcher := events.NewDispatcher()
	if err := dispatcher.OnP2MessageReceive(func(ctx context.Context, event events.MessageEvent) error {
		// Production handlers should durably enqueue slow work and deduplicate
		// event.Header.EventID before performing externally visible actions.
		log.Printf("收到消息事件：%s，类型：%s", event.Header.EventID, event.Message.MessageType)
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	if err := dispatcher.OnCardAction(func(ctx context.Context, event card.CallbackRequest) (*card.CallbackResponse, error) {
		// Authenticate/authorize the operator against business rules here.
		return &card.CallbackResponse{Toast: &card.Toast{Type: "info", Content: "已收到操作"}}, nil
	}); err != nil {
		log.Fatal(err)
	}
	if err := dispatcher.OnChatMemberBotAdded(func(ctx context.Context, event events.BotAddedEvent) error {
		log.Printf("机器人入群事件：%s", event.Header.EventID)
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	client, err := events.New(events.Config{
		AppID: appID, AppSecret: appSecret, Dispatcher: dispatcher,
		OnConnected: func(context.Context) { log.Println("飞书长连接已建立") },
		OnError:     func(_ context.Context, err error) { log.Printf("飞书长连接：%v", err) },
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
