package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/bkcarlos/goparts/llm"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"io"
	"regexp"
	"sync"
)

type ToolHandler func(context.Context, json.RawMessage) (string, error)
type registered struct {
	tool    llm.Tool
	schema  *jsonschema.Schema
	handler ToolHandler
}
type ToolRegistry struct {
	mu                sync.RWMutex
	tools             map[string]registered
	order             []string
	MaxArgumentsBytes int
}

var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (r *ToolRegistry) Register(name, description string, schema json.RawMessage, handler ToolHandler) error {
	if !toolName.MatchString(name) || handler == nil || len(schema) > 1<<20 {
		return errors.New("chat: invalid tool definition")
	}
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(string) (io.ReadCloser, error) {
		return nil, errors.New("chat: external schema references disabled")
	}
	if err := compiler.AddResource("https://goparts.invalid/tool.json", bytes.NewReader(schema)); err != nil {
		return errors.New("chat: invalid schema")
	}
	compiled, err := compiler.Compile("https://goparts.invalid/tool.json")
	if err != nil {
		return errors.New("chat: invalid schema or external reference")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]registered{}
	}
	if _, exists := r.tools[name]; exists {
		return errors.New("chat: duplicate tool")
	}
	r.tools[name] = registered{llm.FunctionTool(name, description, append(json.RawMessage(nil), schema...)), compiled, handler}
	r.order = append(r.order, name)
	return nil
}
func (r *ToolRegistry) Tools() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name].tool
		tool.Function.Parameters = append(json.RawMessage(nil), tool.Function.Parameters...)
		out = append(out, tool)
	}
	return out
}

// Dispatch validates arguments before invoking trusted, explicitly registered
// handlers. It does not authorize tool effects; handlers must enforce permissions.
func (r *ToolRegistry) Dispatch(ctx context.Context, call llm.ToolCall) (llm.Message, error) {
	if ctx == nil {
		return llm.Message{}, errors.New("chat: context required")
	}
	if err := ctx.Err(); err != nil {
		return llm.Message{}, err
	}
	r.mu.RLock()
	tool, ok := r.tools[call.Function.Name]
	r.mu.RUnlock()
	limit := r.MaxArgumentsBytes
	if limit == 0 {
		limit = 1 << 20
	}
	if !ok || call.ID == "" || call.Type != "function" || limit < 0 || len(call.Function.Arguments) > limit {
		return llm.Message{}, errors.New("chat: invalid/unregistered tool call")
	}
	var value any
	dec := json.NewDecoder(bytes.NewBufferString(call.Function.Arguments))
	dec.UseNumber()
	if dec.Decode(&value) != nil {
		return llm.Message{}, errors.New("chat: invalid JSON arguments")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return llm.Message{}, errors.New("chat: trailing JSON")
	}
	if tool.schema.Validate(value) != nil {
		return llm.Message{}, errors.New("chat: arguments do not match schema")
	}
	text, err := tool.handler(ctx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		return llm.Message{}, err
	}
	return llm.ToolResult(call.ID, text), nil
}
