package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const chatJSON = `{"id":"chat-1","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}
func basicRequest() ChatRequest { return ChatRequest{Messages: []Message{User("hello")}} }

func TestChatRequestAndConfigurationIsolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compatible/v1/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Custom") != "original" {
			t.Errorf("request: %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["model"]) != `"test-model"` || string(body["temperature"]) != "0" || string(body["stream"]) != "false" || string(body["enable_thinking"]) != "false" || body["stream_options"] != nil || body["max_tokens"] != nil {
			t.Errorf("payload: %s", body)
		}
		w.Header().Set("X-Request-Id", "req-1")
		io.WriteString(w, chatJSON)
	}))
	defer srv.Close()
	headers := http.Header{"X-Custom": []string{"original"}}
	c := mustClient(t, Config{BaseURL: srv.URL + "/compatible/v1/", Model: "test-model", APIKey: "test-key", Headers: headers})
	headers.Set("X-Custom", "changed")
	zero := 0.0
	input := basicRequest()
	input.Temperature = &zero
	input.Extra = map[string]any{"enable_thinking": false}
	resp, err := c.Chat(context.Background(), input)
	if err != nil || resp.Text() != "你好" || resp.Usage.TotalTokens != 3 || resp.RequestID != "req-1" {
		t.Fatalf("response=%+v error=%v", resp, err)
	}
	if input.Model != "" || len(input.Extra) != 1 {
		t.Fatal("request modified")
	}
}

func TestToolRoundTripAndJSONFormat(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls.Add(1) == 1 {
			if body.Model != "override" || len(body.Tools) != 1 || body.Tools[0].Function.Name != "lookup" || body.ResponseFormat.Type != "json_schema" {
				t.Errorf("body=%+v", body)
			}
			io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":42}"}}]},"finish_reason":"tool_calls"}]}`)
		} else {
			if len(body.Messages) != 3 || body.Messages[1].ToolCalls[0].ID != "call-1" || body.Messages[2].ToolCallID != "call-1" || body.Messages[2].Content != "found" {
				t.Errorf("messages=%+v", body.Messages)
			}
			io.WriteString(w, chatJSON)
		}
	}))
	defer srv.Close()
	c := mustClient(t, Config{BaseURL: srv.URL, Model: "default"})
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)
	input := basicRequest()
	input.Model = "override"
	input.Tools = []Tool{FunctionTool("lookup", "Look up an ID", schema)}
	input.ResponseFormat = &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{Name: "result", Schema: schema}}
	resp, err := c.Chat(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatal("unexpected tool behavior")
	}
	var args struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), &args); err != nil || args.ID != 42 {
		t.Fatalf("arguments: %+v %v", args, err)
	}
	input.Messages = append(input.Messages, resp.Choices[0].Message, ToolResult("call-1", "found"))
	if _, err := c.Chat(context.Background(), input); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorsAndResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
		api    bool
	}{
		{"rate limit", 429, `{"error":{"message":"sensitive prompt","type":"rate_limit_error","code":"limited"}}`, "limited", true},
		{"numeric code", 400, `{"error":{"message":"sensitive prompt","code":123}}`, "123", true},
		{"embedded error", 200, `{"error":{"message":"sensitive prompt"}}`, "", true},
		{"string error", 500, `{"error":"sensitive prompt"}`, "", true},
		{"html error", 502, `<html>sensitive prompt</html>`, "", true},
		{"malformed", 200, `invalid`, "", false},
		{"missing choices", 200, `{}`, "", false},
		{"null", 200, `null`, "", false},
		{"missing message", 200, `{"choices":[{}]}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("X-Request-Id", "req-error")
				w.Header().Set("Retry-After", "3")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			_, err := mustClient(t, Config{BaseURL: srv.URL, Model: "model"}).Chat(context.Background(), basicRequest())
			if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), "sensitive prompt") {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
			var apiErr *APIError
			if tc.api {
				if !errors.As(err, &apiErr) || apiErr.Code != tc.code || apiErr.StatusCode != tc.status || apiErr.RequestID != "req-error" || apiErr.RetryAfter != "3" {
					t.Fatalf("API error: %+v", err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) {
				t.Fatal(err)
			}
		})
	}
}

func TestSizeLimitsRedirectAndClientCopy(t *testing.T) {
	var forwarded atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer dst.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect/chat/completions" {
			http.Redirect(w, r, dst.URL, 307)
			return
		}
		io.WriteString(w, chatJSON)
	}))
	defer srv.Close()
	original := &http.Client{}
	c := mustClient(t, Config{BaseURL: srv.URL + "/redirect", Model: "m", HTTPClient: original})
	_, err := c.Chat(context.Background(), basicRequest())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 307 || forwarded.Load() != 0 || original.CheckRedirect != nil {
		t.Fatalf("redirect: %v", err)
	}
	c = mustClient(t, Config{BaseURL: srv.URL, Model: "m", MaxResponseBytes: 10})
	if _, err := c.Chat(context.Background(), basicRequest()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTimeoutAndCancellation(t *testing.T) {
	c := mustClient(t, Config{BaseURL: "https://example.com/secret-path", Model: "m", Timeout: 10 * time.Millisecond, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}})
	_, err := c.Chat(context.Background(), basicRequest())
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret-path") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Chat(ctx, basicRequest()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEmbeddings(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				if r.URL.Path != "/v1/embeddings" || string(body["encoding_format"]) != `"float"` || string(body["model"]) != `"embedding-model"` {
					t.Errorf("request: %s %v", r.URL.Path, body)
				}
				if valid {
					io.WriteString(w, `{"model":"embedding-model","data":[{"index":1,"embedding":[0.3,0.4]},{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":4,"total_tokens":4}}`)
				} else {
					io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1]},{"index":0,"embedding":[0.2]}]}`)
				}
			}))
			defer srv.Close()
			c := mustClient(t, Config{BaseURL: srv.URL + "/v1", Model: "chat-model"})
			dimensions := 2
			resp, err := c.Embeddings(context.Background(), EmbeddingRequest{Model: "embedding-model", Input: []string{"a", "b"}, Dimensions: &dimensions})
			if valid {
				if err != nil || resp.Data[0].Index != 1 || resp.Usage.TotalTokens != 4 {
					t.Fatalf("response=%+v err=%v", resp, err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) {
				t.Fatal(err)
			}
		})
	}
}

func TestValidationAndConcurrentUse(t *testing.T) {
	for _, cfg := range []Config{{BaseURL: "relative"}, {BaseURL: "https://u:p@example.com"}, {BaseURL: "https://example.com?q=x"}, {BaseURL: "https://example.com/#x"}, {Timeout: -1}, {MaxResponseBytes: -1}, {MaxResponseBytes: math.MaxInt64}, {MaxEventBytes: -1}} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	c := mustClient(t, Config{Model: "model"})
	zero := 0
	one := 1
	requests := []ChatRequest{{}, {Messages: []Message{{Role: "bad"}}}, {Messages: []Message{ToolResult("", "x")}}}
	for _, extra := range []map[string]any{{"stream": true}, {"model": "other"}, {"temperature": 0}, {"stream_options": nil}, {"custom": make(chan int)}} {
		r := basicRequest()
		r.Extra = extra
		requests = append(requests, r)
	}
	r := basicRequest()
	r.MaxTokens = &zero
	requests = append(requests, r)
	r = basicRequest()
	r.MaxTokens = &one
	r.MaxCompletionTokens = &one
	requests = append(requests, r)
	r = basicRequest()
	r.Tools = []Tool{{Type: "function"}}
	requests = append(requests, r)
	r = basicRequest()
	nan := math.NaN()
	r.Temperature = &nan
	requests = append(requests, r)
	for _, r := range requests {
		if _, err := c.Chat(context.Background(), r); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if _, err := c.Chat(nil, basicRequest()); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := mustClient(t, Config{}).Chat(context.Background(), basicRequest()); err == nil {
		t.Fatal("missing model accepted")
	}
	for _, r := range []EmbeddingRequest{{}, {Model: "m", Input: []string{""}}, {Model: "m", Input: []string{"x"}, Dimensions: &zero}, {Model: "m", Input: []string{"x"}, Extra: map[string]any{"encoding_format": "base64"}}} {
		if _, err := c.Embeddings(context.Background(), r); err == nil {
			t.Fatal("invalid embedding request accepted")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, chatJSON) }))
	defer srv.Close()
	c = mustClient(t, Config{BaseURL: srv.URL, Model: "model"})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Chat(context.Background(), basicRequest()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
