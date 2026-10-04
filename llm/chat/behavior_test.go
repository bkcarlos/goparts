package chat

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/bkcarlos/goparts/llm"
)

func TestSessionRejectsInvalidToolSequenceWithoutMutation(t *testing.T) {
	call := func(ids ...string) llm.Message {
		m := llm.Message{Role: llm.RoleAssistant}
		for _, id := range ids {
			m.ToolCalls = append(m.ToolCalls, llm.ToolCall{ID: id})
		}
		return m
	}
	for _, tc := range []struct {
		name     string
		messages []llm.Message
	}{
		{"orphan", []llm.Message{llm.ToolResult("missing", "x")}},
		{"duplicate result", []llm.Message{call("a"), llm.ToolResult("a", "x"), llm.ToolResult("a", "y")}},
		{"duplicate call", []llm.Message{call("a", "a")}},
		{"empty call id", []llm.Message{call("")}},
		{"unfinished calls", []llm.Message{call("a", "b"), llm.ToolResult("a", "x"), llm.User("next")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSession(SessionConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Append(llm.System("pinned"), llm.User("start")); err != nil {
				t.Fatal(err)
			}
			before := s.Messages()
			if err := s.Append(tc.messages...); err == nil {
				t.Fatal("invalid sequence accepted")
			}
			if !reflect.DeepEqual(before, s.Messages()) {
				t.Fatal("failed append mutated session")
			}
		})
	}
}

func TestSessionTokenBudgetAndCopyIsolation(t *testing.T) {
	s, err := NewSession(SessionConfig{MaxTokens: 5, CountTokens: func(messages []llm.Message) int {
		n := 0
		for _, m := range messages {
			n += len(m.Content)
		}
		return n
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(llm.System("s"), llm.User("old")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(llm.User("new")); err != nil {
		t.Fatal(err)
	}
	if got := s.Messages(); len(got) != 2 || got[0].Content != "s" || got[1].Content != "new" {
		t.Fatalf("trim=%+v", got)
	}
	before := s.Messages()
	if err := s.Append(llm.User("oversize")); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Messages()) {
		t.Fatal("budget failure mutated session")
	}
	s.Reset()
	input := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a"}}}
	if err := s.Append(llm.User("u"), input); err != nil {
		t.Fatal(err)
	}
	input.ToolCalls[0].ID = "changed"
	snapshot := s.Messages()
	snapshot[1].ToolCalls[0].ID = "changed again"
	if err := s.Append(llm.ToolResult("a", "r")); err != nil {
		t.Fatalf("caller mutated session: %v", err)
	}
	s.Reset()
	if len(s.Messages()) != 0 {
		t.Fatal("reset failed")
	}
}

func TestToolDispatchRejectsBadCallsBeforeHandler(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)
	for _, tc := range []struct {
		name, id, kind, tool, args string
		limit                      int
	}{
		{"unknown", "a", "function", "unknown", `{"id":1}`, 0},
		{"missing id", "", "function", "lookup", `{"id":1}`, 0},
		{"wrong type", "a", "other", "lookup", `{"id":1}`, 0},
		{"malformed", "a", "function", "lookup", `{`, 0},
		{"trailing JSON", "a", "function", "lookup", `{"id":1}{}`, 0},
		{"trailing garbage", "a", "function", "lookup", `{"id":1}x`, 0},
		{"missing required", "a", "function", "lookup", `{}`, 0},
		{"extra property", "a", "function", "lookup", `{"id":1,"secret":2}`, 0},
		{"wrong type argument", "a", "function", "lookup", `{"id":"1"}`, 0},
		{"size limit", "a", "function", "lookup", `{"id":1}`, 4},
		{"negative limit", "a", "function", "lookup", `{"id":1}`, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ToolRegistry{MaxArgumentsBytes: tc.limit}
			calls := 0
			if err := r.Register("lookup", "", schema, func(context.Context, json.RawMessage) (string, error) { calls++; return "ok", nil }); err != nil {
				t.Fatal(err)
			}
			_, err := r.Dispatch(context.Background(), llm.ToolCall{ID: tc.id, Type: tc.kind, Function: llm.FunctionCall{Name: tc.tool, Arguments: tc.args}})
			if err == nil || calls != 0 {
				t.Fatalf("err=%v handler calls=%d", err, calls)
			}
		})
	}
}

func TestToolRegistryDefinitionCopiesAndHandlerErrors(t *testing.T) {
	var r ToolRegistry
	schema := json.RawMessage(`{"type":"object"}`)
	want := errors.New("operation rejected")
	if err := r.Register("first", "description", schema, func(context.Context, json.RawMessage) (string, error) { return "", want }); err != nil {
		t.Fatal(err)
	}
	schema[0] = 'x'
	if err := r.Register("second", "", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (string, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	tools := r.Tools()
	if len(tools) != 2 || tools[0].Function.Name != "first" || tools[1].Function.Name != "second" {
		t.Fatal(tools)
	}
	tools[0].Function.Parameters[0] = 'x'
	if !json.Valid(r.Tools()[0].Function.Parameters) {
		t.Fatal("schema aliased")
	}
	call := llm.ToolCall{ID: "call", Type: "function", Function: llm.FunctionCall{Name: "first", Arguments: `{}`}}
	if _, err := r.Dispatch(context.Background(), call); !errors.Is(err, want) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Dispatch(ctx, call); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(nil, call); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := r.Register("first", "", json.RawMessage(`{}`), func(context.Context, json.RawMessage) (string, error) { return "", nil }); err == nil {
		t.Fatal("duplicate tool accepted")
	}
}

func TestAccumulatorErrorsLimitsAndClose(t *testing.T) {
	t.Run("flush failure is sticky", func(t *testing.T) {
		want := errors.New("card update failed")
		calls := 0
		a, err := NewAccumulator(context.Background(), AccumulatorConfig{Characters: 1, Flush: func(context.Context, string) error { calls++; return want }})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if err := a.Add("x"); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if err := a.Add("y"); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if err := a.Flush(); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if err := a.Close(); !errors.Is(err, want) {
			t.Fatal(err)
		}
		if calls != 1 || a.Text() != "x" {
			t.Fatalf("calls=%d text=%q", calls, a.Text())
		}
	})
	t.Run("byte bound and rune threshold", func(t *testing.T) {
		var values []string
		a, err := NewAccumulator(context.Background(), AccumulatorConfig{Characters: 2, MaxBytes: 7, Flush: func(_ context.Context, s string) error { values = append(values, s); return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if err := a.Add("你"); err != nil {
			t.Fatal(err)
		}
		if len(values) != 0 {
			t.Fatal("threshold counted bytes instead of runes")
		}
		if err := a.Add("好"); err != nil {
			t.Fatal(err)
		}
		if err := a.Add("ab"); err == nil {
			t.Fatal("overflow accepted")
		}
		if err := a.Add("!"); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(values, []string{"你好", "你好!"}) {
			t.Fatal(values)
		}
		if err := a.Add("x"); err == nil {
			t.Fatal("add after close accepted")
		}
	})
	t.Run("cancel suppresses callback", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		a, err := NewAccumulator(ctx, AccumulatorConfig{Flush: func(context.Context, string) error { t.Error("flushed canceled data"); return nil }})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Add("pending"); err != nil {
			t.Fatal(err)
		}
		cancel()
		if err := a.Close(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestSequenceAllocatorConcurrentUniqueness(t *testing.T) {
	var s SequenceAllocator[string]
	var wg sync.WaitGroup
	results := make(chan uint64, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Next("card") }()
	}
	wg.Wait()
	close(results)
	seen := map[uint64]bool{}
	for n := range results {
		if n < 1 || n > 100 || seen[n] {
			t.Fatalf("duplicate/out-of-range sequence %d", n)
		}
		seen[n] = true
	}
	if len(seen) != 100 || s.Next("other") != 1 {
		t.Fatal("sequence isolation failed")
	}
	s.Forget("card")
	if s.Next("card") != 1 {
		t.Fatal("forget failed")
	}
}

func FuzzToolArguments(f *testing.F) {
	for _, s := range []string{`{}`, `{"id":1}`, `null`, `{"id":1}{}`, "\xff"} {
		f.Add(s)
	}
	var r ToolRegistry
	if err := r.Register("echo", "", json.RawMessage(`{"type":"object"}`), func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil }); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, args string) {
		m, err := r.Dispatch(context.Background(), llm.ToolCall{ID: "call", Type: "function", Function: llm.FunctionCall{Name: "echo", Arguments: args}})
		if err == nil && (!json.Valid([]byte(args)) || !strings.HasPrefix(strings.TrimSpace(args), "{") || m.ToolCallID != "call" || m.Content != args) {
			t.Fatalf("invalid successful dispatch: %+v", m)
		}
	})
}
