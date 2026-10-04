// This example only calls a local test server. No credentials are needed.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/bkcarlos/goparts/feishu"
	"github.com/bkcarlos/goparts/feishu/card"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"mock-tenant-token","expire":7200}`)
		case "/cardkit/v1/cards":
			fmt.Fprint(w, `{"code":0,"data":{"card_id":"123"}}`)
		case "/im/v1/messages":
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_mock","chat_id":"oc_mock"}}`)
		case "/cardkit/v1/cards/123/elements/answer/content", "/cardkit/v1/cards/123/settings", "/im/v1/messages/om_mock", "/webhook":
			fmt.Fprint(w, `{"code":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	client, err := card.New(card.Config{AppID: "mock-app", AppSecret: "mock-secret", BaseURL: server.URL})
	if err != nil {
		return err
	}

	// Same JSON 2.0 builder is usable with the existing webhook client.
	notice := card.NewCard("发布成功").WithSummary("服务已上线").Add(card.Markdown("**版本**：v1.0"), card.Divider(), card.LinkButton("查看发布", "https://example.com/releases"))
	webhook, err := feishu.New(feishu.Config{WebhookURL: server.URL + "/webhook"})
	if err != nil {
		return err
	}
	if err = webhook.Send(ctx, feishu.Card(notice)); err != nil {
		return err
	}
	fmt.Println("Webhook 通知卡片：发送成功（本地模拟）")

	// Create and send an entity, then update its full Markdown text in sequence.
	answer := card.NewCard("AI 回答").WithStreaming(true).Add(card.Markdown("思考中…").WithID("answer"))
	cardID, err := client.Create(ctx, answer)
	if err != nil {
		return err
	}
	message, err := client.Send(ctx, card.Chat("oc_mock"), card.Reference(cardID), card.SendOptions{UUID: "mock-answer-1"})
	if err != nil {
		return err
	}
	var sequence int32
	var fullText strings.Builder
	for _, delta := range []string{"你好，", "这是一段完整回答。"} {
		fullText.WriteString(delta)
		sequence++ // shared by every mutation on this card, including Finish
		if err = client.UpdateText(ctx, cardID, "answer", fullText.String(), card.UpdateOptions{Sequence: sequence}); err != nil {
			return err
		}
	}
	sequence++
	if err = client.Finish(ctx, cardID, "回答完成", card.UpdateOptions{Sequence: sequence}); err != nil {
		return err
	}
	fmt.Printf("流式卡片：card_id=%s message_id=%s，已结束（本地模拟）\n", cardID, message.MessageID)

	// Business code can acknowledge a button and immediately return a new card.
	decoder, err := card.NewCallbackDecoder(card.CallbackConfig{AppID: "mock-app", VerificationToken: "mock-verify"})
	if err != nil {
		return err
	}
	payload := `{"schema":"2.0","header":{"event_id":"ev_mock","event_type":"card.action.trigger","app_id":"mock-app","token":"mock-verify"},"event":{"operator":{"open_id":"ou_mock"},"action":{"tag":"button","value":{"job_id":"42"}},"context":{"open_message_id":"om_mock"}}}`
	event, err := decoder.Decode(httptest.NewRequest("POST", "/callback", strings.NewReader(payload)))
	if err != nil {
		return err
	}
	fmt.Printf("按钮回调：event_id=%s action=%s（本地模拟，无业务变更）\n", event.Header.EventID, event.Event.Action.Tag)
	return json.NewEncoder(os.Stdout).Encode(card.CallbackResponse{
		Toast: &card.Toast{Type: "success", Content: "已确认"},
		Card:  card.RawResponseCard(card.NewCard("已确认").Add(card.Markdown("处理完成"))),
	})
}
