package main

import (
	"context"
	"log"
	"os"

	"github.com/bkcarlos/goparts/feishu"
)

func main() {
	webhook := os.Getenv("FEISHU_WEBHOOK_URL")
	if webhook == "" {
		log.Print("未设置 FEISHU_WEBHOOK_URL，跳过发送")
		return
	}
	bot, err := feishu.New(feishu.Config{
		WebhookURL: webhook,
		Secret:     os.Getenv("FEISHU_SECRET"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := bot.SendMarkdown(context.Background(), "服务通知", "**示例服务已启动**"); err != nil {
		log.Fatal(err)
	}
	log.Print("飞书通知已发送")
}
