package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := New(Config{AppID: "app", AppSecret: "secret", BaseURL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func tokenResponse(w http.ResponseWriter) {
	fmt.Fprint(w, `{"code":0,"tenant_access_token":"tenant","expire":7200}`)
}

func TestLifecycleWireProtocol(t *testing.T) {
	var calls []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/auth/v3/tenant_access_token/internal" {
			if string(body["app_id"]) != `"app"` || string(body["app_secret"]) != `"secret"` || r.Header.Get("Authorization") != "" {
				t.Error("invalid token request")
			}
			tokenResponse(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tenant" {
			t.Error("missing tenant token")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/cardkit/v1/cards":
			var inner string
			if err := json.Unmarshal(body["data"], &inner); err != nil || !json.Valid([]byte(inner)) {
				t.Error("entity data must be serialized JSON")
			}
			fmt.Fprint(w, `{"code":0,"data":{"card_id":"123"}}`)
		case "/im/v1/messages", "/im/v1/messages/om_1/reply", "/im/v1/messages/om_1":
			var inner string
			if err := json.Unmarshal(body["content"], &inner); err != nil || !json.Valid([]byte(inner)) {
				t.Error("content must be serialized JSON")
			}
			if r.URL.Path == "/im/v1/messages" {
				if r.URL.Query().Get("receive_id_type") != "chat_id" || string(body["receive_id"]) != `"oc_1"` || string(body["uuid"]) != `"send-1"` {
					t.Error("invalid send")
				}
				if inner != `{"data":{"card_id":"123"},"type":"card"}` {
					t.Errorf("reference=%s", inner)
				}
			}
			if strings.HasSuffix(r.URL.Path, "/reply") && string(body["reply_in_thread"]) != "true" {
				t.Error("missing thread flag")
			}
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_1","chat_id":"oc_1"}}`)
		case "/cardkit/v1/cards/123/elements/answer/content":
			if string(body["content"]) != `"你好，完整输出"` || string(body["sequence"]) != "1" {
				t.Error("invalid streaming update")
			}
			fmt.Fprint(w, `{"code":0}`)
		case "/cardkit/v1/cards/123/settings":
			var inner string
			_ = json.Unmarshal(body["settings"], &inner)
			if !strings.Contains(inner, `"streaming_mode":false`) || string(body["sequence"]) != "2" {
				t.Error("invalid finish")
			}
			fmt.Fprint(w, `{"code":0}`)
		case "/cardkit/v1/cards/123":
			var card cardData
			_ = json.Unmarshal(body["card"], &card)
			if card.Type != "card_json" || !json.Valid([]byte(card.Data)) || string(body["sequence"]) != "3" {
				t.Error("invalid full update")
			}
			fmt.Fprint(w, `{"code":0}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	content := NewCard("回答").WithStreaming(true).Add(Markdown("思考中").WithID("answer"))
	id, err := c.Create(ctx, content)
	if err != nil || id != "123" {
		t.Fatalf("create: %s %v", id, err)
	}
	m, err := c.Send(ctx, Chat("oc_1"), Reference(id), SendOptions{UUID: "send-1"})
	if err != nil || m.MessageID != "om_1" {
		t.Fatalf("send: %+v %v", m, err)
	}
	if err = c.UpdateText(ctx, id, "answer", "你好，完整输出", UpdateOptions{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err = c.Finish(ctx, id, "回答完成", UpdateOptions{Sequence: 2}); err != nil {
		t.Fatal(err)
	}
	content.WithStreaming(false)
	if err = c.Update(ctx, id, content, UpdateOptions{Sequence: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Reply(ctx, m.MessageID, Template("tpl", "1.0", map[string]any{"text": "完成"}), ReplyOptions{InThread: true}); err != nil {
		t.Fatal(err)
	}
	if err = c.UpdateMessage(ctx, m.MessageID, content); err != nil {
		t.Fatal(err)
	}
	if _, err = c.CreateTemplate(ctx, TemplateData{ID: "tpl", Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	want := "POST /cardkit/v1/cards|POST /im/v1/messages|PUT /cardkit/v1/cards/123/elements/answer/content|PATCH /cardkit/v1/cards/123/settings|PUT /cardkit/v1/cards/123|POST /im/v1/messages/om_1/reply|PATCH /im/v1/messages/om_1|POST /cardkit/v1/cards"
	if strings.Join(calls, "|") != want {
		t.Fatalf("calls=%v", calls)
	}
}

func TestTokenConcurrencyRefreshAndCancellation(t *testing.T) {
	var tokens atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			tokens.Add(1)
			tokenResponse(w)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_1"}}`)
	})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Send(context.Background(), User("ou_1"), NewCard("title"), SendOptions{})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if tokens.Load() != 1 {
		t.Fatalf("token calls=%d", tokens.Load())
	}
	c.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	if _, err := c.accessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 {
		t.Fatal("token did not refresh")
	}
	c.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.accessToken(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting lock: %v", err)
	}
	<-c.gate
}

func TestFailuresAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		api        bool
	}{
		{"business", `{"code":230020,"msg":"limited"}`, 200, true},
		{"http", `bad gateway`, 502, true},
		{"missing_code", `{"data":{"message_id":"om_1"}}`, 200, false},
		{"missing_id", `{"code":0,"data":{}}`, 200, false},
		{"null_data", `{"code":0,"data":null}`, 200, false},
		{"malformed", `{`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					tokenResponse(w)
					return
				}
				calls++
				w.Header().Set("X-Tt-Logid", "request-1")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := c.Send(context.Background(), User("ou_1"), NewCard("title"), SendOptions{})
			var apiErr *APIError
			if tc.api {
				if !errors.As(err, &apiErr) || apiErr.RequestID != "request-1" {
					t.Fatalf("error=%v", err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("error=%v", err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestRedirectAndLimits(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	if _, err := c.accessToken(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
	if leaked.Load() != 0 {
		t.Fatal("credentials forwarded")
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", responseLimit+1)) })
	if _, err := c.accessToken(context.Background()); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestRejectInvalidInputBeforeNetwork(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network call") })
	ctx := context.Background()
	checks := []func() error{
		func() error {
			_, e := c.Send(ctx, Receiver{Type: "bad", ID: "1"}, NewCard(""), SendOptions{})
			return e
		},
		func() error { _, e := c.Send(ctx, User(""), NewCard(""), SendOptions{}); return e },
		func() error { _, e := c.Send(ctx, User("1"), nil, SendOptions{}); return e },
		func() error {
			_, e := c.Send(ctx, User("1"), map[string]any{"x": make(chan int)}, SendOptions{})
			return e
		},
		func() error {
			_, e := c.Send(ctx, User("1"), NewCard(strings.Repeat("x", 31*1024)), SendOptions{})
			return e
		},
		func() error { _, e := c.Reply(ctx, "../token", NewCard(""), ReplyOptions{}); return e },
		func() error { return c.UpdateMessage(ctx, "../token", NewCard("")) },
		func() error { _, e := c.Create(ctx, map[string]string{"schema": "1.0"}); return e },
		func() error { v := NewCard(""); v.Config.UpdateMulti = false; _, e := c.Create(ctx, v); return e },
		func() error { _, e := c.CreateTemplate(ctx, TemplateData{}); return e },
		func() error { return c.Update(ctx, "123", NewCard(""), UpdateOptions{}) },
		func() error { return c.UpdateText(ctx, "123", "bad/id", "x", UpdateOptions{Sequence: 1}) },
		func() error { return c.UpdateText(ctx, "123", "answer", "", UpdateOptions{Sequence: 1}) },
		func() error { return c.Settings(ctx, "123", nil, UpdateOptions{Sequence: 1}) },
		func() error { _, e := c.accessToken(nil); return e },
	}
	for i, f := range checks {
		if err := f(); err == nil {
			t.Errorf("check %d accepted invalid input", i)
		}
	}
}

func TestBuilders(t *testing.T) {
	c := NewCard("发布").WithSummary("上线完成").Add(Markdown("**完成**"), Divider(), Image("img_key", "图"), LinkButton("打开", "https://example.com"), CallbackButton("确认", map[string]any{"job_id": "42"}).WithID("confirm"))
	c.Header.Template = "green"
	data, err := c.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"schema":"2.0"`, `"update_multi":true`, `"tag":"markdown"`, `"default_url":"https://example.com"`, `"type":"callback"`, `"job_id":"42"`, `"element_id":"confirm"`} {
		if !strings.Contains(string(data), part) {
			t.Errorf("missing %s", part)
		}
	}
	if _, err := encodeContent(json.RawMessage(data)); err != nil {
		t.Fatal(err)
	}
}
