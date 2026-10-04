package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bkcarlos/goparts/feishu/card"
	"github.com/gorilla/websocket"
)

func websocketServer(t *testing.T, serve func(*websocket.Conn), bootstrap func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/callback/ws/endpoint":
			var input map[string]string
			if json.NewDecoder(r.Body).Decode(&input) != nil || input["AppID"] != "app" || input["AppSecret"] != "secret" || r.Method != "POST" {
				t.Error("invalid bootstrap")
			}
			if bootstrap != nil && bootstrap(w, r) {
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"URL": "ws://" + r.Host + "/socket?service_id=1&ticket=private-token", "ClientConfig": map[string]int{"PingInterval": 1, "ReconnectInterval": 1, "ReconnectCount": 3}}})
		case "/socket":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			serve(conn)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
func readWire(t *testing.T, conn *websocket.Conn) (frame, error) {
	t.Helper()
	kind, raw, err := conn.ReadMessage()
	if err != nil {
		return frame{}, err
	}
	if kind != websocket.BinaryMessage {
		return frame{}, errProtocol
	}
	return decodeFrame(raw)
}
func sendWire(conn *websocket.Conn, f frame) error {
	return conn.WriteMessage(websocket.BinaryMessage, f.marshal())
}

func TestLocalWebSocketEventsAndCardAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var messages, bots, cards atomic.Int32
	var acks atomic.Int32
	d := NewDispatcher()
	if err := d.OnP2MessageReceive(func(ctx context.Context, e MessageEvent) error {
		if e.Message.MessageID != "m1" || e.Message.Content != `{"text":"hello"}` || e.Header.EventID != "message" {
			t.Errorf("message=%+v", e)
		}
		messages.Add(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.OnChatMemberBotAdded(func(ctx context.Context, e BotAddedEvent) error {
		if e.ChatID != "chat1" {
			t.Error("chat lost")
		}
		bots.Add(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.OnCardAction(func(ctx context.Context, e card.CallbackRequest) (*card.CallbackResponse, error) {
		if e.Event.Action.Tag != "button" || e.Header.EventID != "card" {
			t.Errorf("card=%+v", e)
		}
		cards.Add(1)
		return &card.CallbackResponse{Toast: &card.Toast{Type: "success", Content: "done"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := websocketServer(t, func(conn *websocket.Conn) {
		ping, err := readWire(t, conn)
		if err != nil {
			t.Error(err)
			return
		}
		if ping.method != 0 || ping.service != 1 || ping.get("type") != "ping" {
			t.Error("invalid initial ping")
		}
		if err := sendWire(conn, frame{service: 1, headers: []header{{"type", "pong"}}, payload: []byte(`{"PingInterval":1}`)}); err != nil {
			t.Error(err)
			return
		}
		message := eventFrame("message", MessageReceive, `{"message":{"message_id":"m1","chat_id":"chat1","content":"{\"text\":\"hello\"}"}}`)
		payload := message.payload
		mid := len(payload) / 2
		message.set("sum", "2")
		message.set("seq", "1")
		message.payload = payload[mid:]
		if err := sendWire(conn, message); err != nil {
			t.Error(err)
			return
		}
		message.set("seq", "0")
		message.payload = payload[:mid]
		for _, f := range []frame{message, eventFrame("bot", ChatMemberBotAdded, `{"chat_id":"chat1"}`), eventFrame("card", CardAction, `{"action":{"tag":"button","value":{"action":"ok"}}}`)} {
			if err := sendWire(conn, f); err != nil {
				t.Error(err)
				return
			}
		}
		for acks.Load() < 3 {
			ack, err := readWire(t, conn)
			if err != nil {
				t.Error(err)
				return
			}
			if ack.method == 0 {
				continue
			}
			var response struct {
				Code int    `json:"code"`
				Data []byte `json:"data"`
			}
			if err := json.Unmarshal(ack.payload, &response); err != nil || response.Code != 200 || ack.seq != 7 || ack.log != 9 || ack.get("trace_id") != "trace1" || ack.get("biz_rt") == "" {
				t.Errorf("ack=%+v response=%s err=%v", ack, ack.payload, err)
			}
			if ack.get("message_id") == "card" {
				var callback card.CallbackResponse
				if json.Unmarshal(response.Data, &callback) != nil || callback.Toast == nil || callback.Toast.Content != "done" {
					t.Errorf("invalid base64 response %s", ack.payload)
				}
			}
			acks.Add(1)
		}
		cancel()
	}, nil)
	c, err := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, Dispatcher: d, DisableReconnect: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run=%v", err)
	}
	if messages.Load() != 1 || bots.Load() != 1 || cards.Load() != 1 || acks.Load() != 3 {
		t.Fatal("missing event or ACK")
	}
}

func TestReconnectRefetchesEndpointAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var bootstraps, connections, ready atomic.Int32
	srv := websocketServer(t, func(conn *websocket.Conn) {
		n := connections.Add(1)
		if _, err := readWire(t, conn); err != nil {
			t.Error(err)
			return
		}
		if n == 1 {
			return
		} // deliberately drop the physical connection
		cancel()
	}, func(http.ResponseWriter, *http.Request) bool { bootstraps.Add(1); return false })
	c, err := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, Dispatcher: NewDispatcher(), ReconnectInterval: time.Millisecond, OnConnected: func(context.Context) { ready.Add(1) }, OnError: func(_ context.Context, err error) {
		if strings.Contains(err.Error(), "private-token") {
			t.Error("secret URL leaked")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if bootstraps.Load() != 2 || connections.Load() != 2 || ready.Load() != 2 {
		t.Fatalf("bootstrap=%d connections=%d ready=%d", bootstraps.Load(), connections.Load(), ready.Load())
	}
}

func TestPermanentErrorsRetryBudgetAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status, code, calls int
	}{
		{"invalid credentials", 200, 1000040344, 1}, {"forbidden", 403, 403, 1}, {"busy", 200, 1, 3}, {"rate limited", 429, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("X-Tt-Logid", "req1")
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"code":%d,"msg":"private-token"}`, tc.code)
			}))
			defer srv.Close()
			c, err := New(Config{AppID: "app", AppSecret: "secret", Dispatcher: NewDispatcher(), BaseURL: srv.URL, MaxReconnectAttempts: 2, ReconnectInterval: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = c.Run(ctx)
			var api *APIError
			if !errors.As(err, &api) || api.RequestID != "req1" || calls.Load() != int32(tc.calls) || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
		})
	}
	for _, body := range []string{`{}`, `{"code":0,"data":null}`, `{"code":0,"data":{"URL":"ws://example.com/?service_id=bad"}}`, strings.Repeat("x", 1025)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		c, _ := New(Config{AppID: "app", AppSecret: "secret", Dispatcher: NewDispatcher(), BaseURL: srv.URL, MaxResponseBytes: 1024})
		if err := c.Run(context.Background()); !errors.Is(err, errProtocol) {
			t.Errorf("body=%s err=%v", body, err)
		}
		srv.Close()
	}
}

func TestRunExclusionAndCancellationJoinsHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, exited := make(chan struct{}), make(chan struct{})
	d := NewDispatcher()
	d.On("wait", func(ctx context.Context, _ Event) (any, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	srv := websocketServer(t, func(conn *websocket.Conn) {
		if _, err := readWire(t, conn); err != nil {
			return
		}
		sendWire(conn, eventFrame("wait", "wait", `{}`))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}, nil)
	c, _ := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, Dispatcher: d})
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}
	if err := c.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run failed to stop")
	}
	select {
	case <-exited:
	default:
		t.Fatal("Run returned without joining handler")
	}
}

func TestConfigAndBootstrapCancellation(t *testing.T) {
	base := Config{AppID: "app", AppSecret: "secret", Dispatcher: NewDispatcher()}
	for _, modify := range []func(*Config){
		func(c *Config) { c.AppSecret = "" }, func(c *Config) { c.Dispatcher = nil }, func(c *Config) { c.BaseURL = "https://example.com/open-apis" }, func(c *Config) { c.BaseURL = "https://u:p@example.com" }, func(c *Config) { c.MaxFrameBytes = -1 }, func(c *Config) { c.HandlerTimeout = -1 }, func(c *Config) { c.QueueSize = -1 },
	} {
		cfg := base
		modify(&cfg)
		if _, err := New(cfg); err == nil {
			t.Error("invalid config accepted")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer srv.Close()
	base.BaseURL = srv.URL
	base.ConnectTimeout = 10 * time.Millisecond
	base.DisableReconnect = true
	c, _ := New(base)
	if err := c.Run(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bootstrap cancellation: %v", err)
	}
}

func TestPongUpdatesHeartbeatAndReconnectPolicy(t *testing.T) {
	var connections atomic.Int32
	srv := websocketServer(t, func(conn *websocket.Conn) {
		connections.Add(1)
		if _, err := readWire(t, conn); err != nil {
			t.Error(err)
			return
		}
		if err := sendWire(conn, frame{service: 1, headers: []header{{"type", "pong"}}, payload: []byte(`{"PingInterval":1,"ReconnectCount":0}`)}); err != nil {
			t.Error(err)
			return
		}
		// A second heartbeat demonstrates that pong configuration is applied.
		ping, err := readWire(t, conn)
		if err != nil || ping.get("type") != "ping" {
			t.Errorf("ping=%+v err=%v", ping, err)
		}
		// Drop socket. The last pong explicitly disabled further reconnects.
	}, nil)
	c, _ := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, Dispatcher: NewDispatcher(), ReconnectInterval: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := c.Run(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) || connections.Load() != 1 {
		t.Fatalf("err=%v connections=%d", err, connections.Load())
	}
}

func TestQueueBoundAndFrameLimit(t *testing.T) {
	for _, oversize := range []bool{false, true} {
		t.Run(fmt.Sprint(oversize), func(t *testing.T) {
			entered := make(chan struct{})
			d := NewDispatcher()
			d.On("wait", func(ctx context.Context, _ Event) (any, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() })
			srv := websocketServer(t, func(conn *websocket.Conn) {
				if _, err := readWire(t, conn); err != nil {
					return
				}
				if oversize {
					conn.WriteMessage(websocket.BinaryMessage, make([]byte, 2048))
				} else {
					if err := sendWire(conn, eventFrame("a", "wait", `{}`)); err != nil {
						return
					}
					select {
					case <-entered:
					case <-time.After(time.Second):
						t.Error("handler never entered")
						return
					}
					for _, id := range []string{"b", "c"} {
						if err := sendWire(conn, eventFrame(id, "wait", `{}`)); err != nil {
							return
						}
					}
				}
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}, nil)
			c, _ := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, Dispatcher: d, QueueSize: 1, MaxFrameBytes: 1024, DisableReconnect: true})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := c.Run(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("overflow did not close connection: %v", err)
			}
		})
	}
}
