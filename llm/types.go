// Package llm implements the OpenAI-compatible Chat Completions and Embeddings APIs.
package llm

import "encoding/json"

const (
	RoleSystem    = "system"
	RoleDeveloper = "developer"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message supports text conversations and function-tool results. Model-generated
// function arguments must be validated by the caller before executing any tool.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	Name             string     `json:"name,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	Refusal          string     `json:"refusal,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // provider extension
}

func System(content string) Message    { return Message{Role: RoleSystem, Content: content} }
func User(content string) Message      { return Message{Role: RoleUser, Content: content} }
func Assistant(content string) Message { return Message{Role: RoleAssistant, Content: content} }
func ToolResult(callID, content string) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Content: content}
}

type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

func FunctionTool(name, description string, parameters json.RawMessage) Tool {
	return Tool{Type: "function", Function: FunctionDefinition{Name: name, Description: description, Parameters: parameters}}
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON text, not guaranteed to be valid
}

type ResponseFormat struct {
	Type       string      `json:"type"` // text, json_object or json_schema; provider/model-dependent
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ChatRequest struct {
	Model               string          `json:"model,omitempty"` // falls back to Config.Model
	Messages            []Message       `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"` // legacy compatible providers
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"` // string or a named-tool JSON object
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	IncludeUsage        bool            `json:"-"` // ChatStream only; opt-in for provider compatibility
	Extra               map[string]any  `json:"-"` // top-level extensions; cannot replace built-in fields
}

type Usage struct {
	PromptTokens            int             `json:"prompt_tokens"`
	CompletionTokens        int             `json:"completion_tokens"`
	TotalTokens             int             `json:"total_tokens"`
	PromptTokensDetails     json.RawMessage `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails json.RawMessage `json:"completion_tokens_details,omitempty"`
}

type ChatResponse struct {
	ID        string   `json:"id"`
	Model     string   `json:"model"`
	Choices   []Choice `json:"choices"`
	Usage     *Usage   `json:"usage,omitempty"`
	RequestID string   `json:"-"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Text returns choice zero's text. Inspect Choices for tools, refusals and finish reasons.
func (r *ChatResponse) Text() string {
	if r == nil {
		return ""
	}
	for _, c := range r.Choices {
		if c.Index == 0 {
			return c.Message.Content
		}
	}
	return ""
}

type ChatChunk struct {
	ID        string        `json:"id"`
	Model     string        `json:"model"`
	Choices   []ChunkChoice `json:"choices"`
	Usage     *Usage        `json:"usage,omitempty"` // final usage-only chunks may have empty choices
	RequestID string        `json:"-"`
}

type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	Refusal          string          `json:"refusal,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta contains fragments. Accumulate by choice index and tool index;
// function arguments are often incomplete JSON until the stream finishes.
type ToolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type EmbeddingRequest struct {
	Model      string         `json:"model"` // required; does not inherit a chat model
	Input      []string       `json:"input"`
	Dimensions *int           `json:"dimensions,omitempty"`
	Extra      map[string]any `json:"-"`
}

type EmbeddingResponse struct {
	Model     string      `json:"model"`
	Data      []Embedding `json:"data"`
	Usage     *Usage      `json:"usage,omitempty"`
	RequestID string      `json:"-"`
}

type Embedding struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}
