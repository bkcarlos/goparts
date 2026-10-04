package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const textChunk = `{"id":"chat-1","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":null}]}`

func streamServer(t *testing.T, body string, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Request-Id", "stream-id")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamTextToolsAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&req)
		if string(req["stream"]) != "true" || string(req["stream_options"]) != `{"include_usage":true}` || r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("request: %v", req)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("X-Request-Id", "stream-id")
		io.WriteString(w, ": heartbeat\r\n\r\nevent: message\r\nid: 1\r\ndata: {\"choices\":[{\"index\":0,\r\ndata: \"delta\":{\"role\":\"assistant\",\"content\":\"你好\"}}]}\r\n\r\n")
		io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":"}},{"index":1,"id":"call-2","function":{"name":"other","arguments":"{}"}}]}}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"42}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		io.WriteString(w, "data: "+`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "model"})
	req := basicRequest()
	req.IncludeUsage = true
	var text string
	args := map[int]string{}
	var usage *Usage
	var finish string
	err := c.ChatStream(context.Background(), req, func(chunk ChatChunk) error {
		if chunk.RequestID != "stream-id" {
			t.Error("missing request ID")
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			text += choice.Delta.Content
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
			for _, call := range choice.Delta.ToolCalls {
				args[call.Index] += call.Function.Arguments
			}
		}
		return nil
	})
	if err != nil || text != "你好" || args[0] != `{"id":42}` || args[1] != `{}` || usage == nil || usage.TotalTokens != 8 || finish != "tool_calls" {
		t.Fatalf("text=%s args=%v usage=%v finish=%s error=%v", text, args, usage, finish, err)
	}
}

func TestStreamProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		want                    error
		api                     bool
	}{
		{"empty", "", "text/event-stream", io.ErrUnexpectedEOF, false},
		{"truncated", "data: " + textChunk + "\n\n", "text/event-stream", io.ErrUnexpectedEOF, false},
		{"unfinished event", "data: " + textChunk, "text/event-stream", io.ErrUnexpectedEOF, false},
		{"invalid JSON", "data: nope\n\n", "text/event-stream", ErrInvalidResponse, false},
		{"missing fields", "data: {}\n\n", "text/event-stream", ErrInvalidResponse, false},
		{"only done", "data: [DONE]\n\n", "text/event-stream", ErrInvalidResponse, false},
		{"wrong type", chatJSON, "application/json", ErrInvalidResponse, false},
		{"error event", "event: error\ndata: {\"error\":{\"message\":\"failed\",\"code\":\"stream_error\"}}\n\n", "text/event-stream", nil, true},
		{"JSON error", `{"error":{"message":"failed"}}`, "application/json", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := streamServer(t, tc.body, tc.contentType)
			c := mustClient(t, Config{BaseURL: srv.URL, Model: "model"})
			err := c.ChatStream(context.Background(), basicRequest(), func(ChatChunk) error { return nil })
			if tc.api {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("got=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestLargeChunksAndMultilineLimit(t *testing.T) {
	chunk := `{"choices":[{"index":0,"delta":{"content":"` + strings.Repeat("x", 70*1024) + `"}}]}`
	srv := streamServer(t, "data: "+chunk+"\n\ndata: [DONE]\n\n", "text/event-stream")
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "model"})
	size := 0
	if err := c.ChatStream(context.Background(), basicRequest(), func(chunk ChatChunk) error { size += len(chunk.Choices[0].Delta.Content); return nil }); err != nil || size != 70*1024 {
		t.Fatalf("size=%d err=%v", size, err)
	}
	c = mustClient(t, Config{BaseURL: srv.URL, Model: "model", MaxEventBytes: 100})
	if err := c.ChatStream(context.Background(), basicRequest(), func(ChatChunk) error { return nil }); !errors.Is(err, ErrEventTooLarge) {
		t.Fatal(err)
	}
	multi := streamServer(t, strings.Repeat("data: small\n", 20)+"\n", "text/event-stream")
	c = mustClient(t, Config{BaseURL: multi.URL, Model: "model", MaxEventBytes: 100})
	if err := c.ChatStream(context.Background(), basicRequest(), func(ChatChunk) error { return nil }); !errors.Is(err, ErrEventTooLarge) {
		t.Fatal(err)
	}
}

func TestStreamCallbackAndCancellation(t *testing.T) {
	srv := streamServer(t, "data: "+textChunk+"\n\ndata: "+textChunk+"\n\ndata: [DONE]\n\n", "text/event-stream")
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "model"})
	want := errors.New("stop callback")
	if err := c.ChatStream(context.Background(), basicRequest(), func(ChatChunk) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	if err := c.ChatStream(ctx, basicRequest(), func(ChatChunk) error { calls++; cancel(); return nil }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	if err := c.ChatStream(context.Background(), basicRequest(), nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": waiting\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer blocked.Close()
	c = mustClient(t, Config{BaseURL: blocked.URL, Model: "model", Timeout: 20 * time.Millisecond})
	if err := c.ChatStream(context.Background(), basicRequest(), func(ChatChunk) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}
