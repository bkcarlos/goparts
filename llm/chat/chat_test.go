package chat

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bkcarlos/goparts/llm"
	"sync"
	"testing"
	"time"
)

func TestTrimKeepsToolTurnsAtomic(t *testing.T) {
	s, _ := NewSession(SessionConfig{MaxMessages: 4})
	call := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call"}}}
	if err := s.Append(llm.System("system"), llm.User("first"), call, llm.ToolResult("call", "result")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(llm.User("second")); err != nil {
		t.Fatal(err)
	}
	msgs := s.Messages()
	if len(msgs) != 2 || msgs[1].Content != "second" {
		t.Fatal(msgs)
	}
	if s.Append(llm.ToolResult("orphan", "bad")) == nil {
		t.Fatal("orphan accepted")
	}
	small, _ := NewSession(SessionConfig{MaxMessages: 1})
	if !errors.Is(small.Append(llm.System("pinned"), llm.User("large")), ErrBudget) {
		t.Fatal("budget")
	}
}
func TestToolsValidateBeforeExecuting(t *testing.T) {
	var r ToolRegistry
	calls := 0
	err := r.Register("lookup", "", json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`), func(context.Context, json.RawMessage) (string, error) { calls++; return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{ID: "a", Type: "function", Function: llm.FunctionCall{Name: "lookup", Arguments: `{"id":"bad"}`}}
	if _, err = r.Dispatch(context.Background(), call); err == nil || calls != 0 {
		t.Fatal("invalid args executed")
	}
	call.Function.Arguments = `{"id":1}`
	m, err := r.Dispatch(context.Background(), call)
	if err != nil || m.ToolCallID != "a" || calls != 1 {
		t.Fatal(m, err)
	}
	if r.Register("remote", "", json.RawMessage(`{"$ref":"file:///etc/passwd"}`), func(context.Context, json.RawMessage) (string, error) { return "", nil }) == nil {
		t.Fatal("external reference allowed")
	}
}
func TestAccumulatorThresholdTimerClose(t *testing.T) {
	var mu sync.Mutex
	var values []string
	flushed := make(chan struct{}, 10)
	a, err := NewAccumulator(context.Background(), AccumulatorConfig{Characters: 2, Interval: time.Millisecond, Flush: func(_ context.Context, s string) error {
		mu.Lock()
		values = append(values, s)
		mu.Unlock()
		flushed <- struct{}{}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	a.Add("你好")
	<-flushed
	a.Add("x")
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("timer didn't flush")
	}
	a.Add("y")
	a.Close()
	mu.Lock()
	defer mu.Unlock()
	if values[len(values)-1] != "你好xy" {
		t.Fatal(values)
	}
}
