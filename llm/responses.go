package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
)

type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}
type ResponseInput struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	Content any    `json:"content,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Output  string `json:"output,omitempty"`
}
type ResponseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}
type ResponsesRequest struct {
	Model              string         `json:"model,omitempty"`
	Input              any            `json:"input"`
	Instructions       string         `json:"instructions,omitempty"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
	Reasoning          *Reasoning     `json:"reasoning,omitempty"`
	Tools              []ResponseTool `json:"tools,omitempty"`
	ToolChoice         any            `json:"tool_choice,omitempty"`
	MaxOutputTokens    *int           `json:"max_output_tokens,omitempty"`
	Store              *bool          `json:"store,omitempty"`
	Extra              map[string]any `json:"-"`
}
type ResponseContent struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	Refusal     string          `json:"refusal,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
}
type ResponseItem struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Role      string            `json:"role,omitempty"`
	Status    string            `json:"status,omitempty"`
	Content   []ResponseContent `json:"content,omitempty"`
	CallID    string            `json:"call_id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Arguments string            `json:"arguments,omitempty"`
	Summary   json.RawMessage   `json:"summary,omitempty"`
}
type ResponseUsage struct {
	InputTokens   int             `json:"input_tokens"`
	OutputTokens  int             `json:"output_tokens"`
	TotalTokens   int             `json:"total_tokens"`
	InputDetails  json.RawMessage `json:"input_tokens_details,omitempty"`
	OutputDetails json.RawMessage `json:"output_tokens_details,omitempty"`
}
type ResponseFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ResponsesResponse struct {
	ID                string           `json:"id"`
	Model             string           `json:"model"`
	Status            string           `json:"status"`
	Output            []ResponseItem   `json:"output"`
	Usage             *ResponseUsage   `json:"usage,omitempty"`
	Error             *ResponseFailure `json:"error,omitempty"`
	IncompleteDetails json.RawMessage  `json:"incomplete_details,omitempty"`
	RequestID         string           `json:"-"`
}

func (r *ResponsesResponse) Text() string {
	if r == nil {
		return ""
	}
	var out strings.Builder
	for _, item := range r.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				out.WriteString(part.Text)
			}
		}
	}
	return out.String()
}

// ResponseStatusError preserves provider diagnostics without logging them.
type ResponseStatusError struct {
	ResponseID, Status string
	Failure            *ResponseFailure
	IncompleteDetails  json.RawMessage
}

func (e *ResponseStatusError) Error() string { return "llm: response did not complete successfully" }
func (e *ResponseStatusError) ErrorInfo() (string, string) {
	return "llm.response_incomplete", "model response did not complete"
}
func (c *Client) responsesBody(input ResponsesRequest, stream bool) ([]byte, error) {
	if input.Model == "" {
		input.Model = c.model
	}
	if strings.TrimSpace(input.Model) == "" || input.Input == nil {
		return nil, errors.New("llm: model and input required")
	}
	if input.MaxOutputTokens != nil && *input.MaxOutputTokens <= 0 {
		return nil, errors.New("llm: max output tokens must be positive")
	}
	return encode(input, input.Extra, map[string]any{"stream": stream})
}
func responseStatus(r *ResponsesResponse) error {
	if r == nil || r.ID == "" || r.Status == "" {
		return ErrInvalidResponse
	}
	if r.Status != "completed" {
		return &ResponseStatusError{r.ID, r.Status, r.Error, r.IncompleteDetails}
	}
	return nil
}
func (c *Client) Responses(ctx context.Context, input ResponsesRequest) (*ResponsesResponse, error) {
	body, err := c.responsesBody(input, false)
	if err != nil {
		return nil, err
	}
	var result ResponsesResponse
	result.RequestID, err = c.postJSON(ctx, "/responses", body, &result)
	if err != nil {
		return nil, err
	}
	return &result, responseStatus(&result)
}

type ResponseEvent struct {
	Type           string             `json:"type"`
	SequenceNumber int64              `json:"sequence_number"`
	Response       *ResponsesResponse `json:"response,omitempty"`
	Delta          string             `json:"delta,omitempty"`
	ItemID         string             `json:"item_id,omitempty"`
	OutputIndex    int                `json:"output_index,omitempty"`
	ContentIndex   int                `json:"content_index,omitempty"`
	Item           *ResponseItem      `json:"item,omitempty"`
	Code           string             `json:"code,omitempty"`
	Message        string             `json:"message,omitempty"`
	Raw            json.RawMessage    `json:"-"`
	RequestID      string             `json:"-"`
}

// ResponsesStream succeeds only on response.completed, never on bare EOF. All
// events, including unknown future event types, retain Raw and reach onEvent.
func (c *Client) ResponsesStream(ctx context.Context, input ResponsesRequest, onEvent func(ResponseEvent) error) error {
	if onEvent == nil {
		return errors.New("llm: event callback required")
	}
	body, err := c.responsesBody(input, true)
	if err != nil {
		return err
	}
	resp, cancel, err := c.request(ctx, "/responses", body, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" {
		return ErrInvalidResponse
	}
	reader := bufio.NewReader(resp.Body)
	var data strings.Builder
	eventName := ""
	size := 0
	for {
		if err := resp.Request.Context().Err(); err != nil {
			return err
		}
		line, err := readLine(reader, c.maxEventBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return withoutURL(err)
		}
		if line != "" {
			size += len(line) + 1
			if size > c.maxEventBytes {
				return ErrEventTooLarge
			}
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				eventName = value
			case "data":
				data.WriteString(value)
				data.WriteByte('\n')
			}
			continue
		}
		payload := strings.TrimSuffix(data.String(), "\n")
		data.Reset()
		size = 0
		name := eventName
		eventName = ""
		if payload == "" {
			continue
		}
		var event ResponseEvent
		if json.Unmarshal([]byte(payload), &event) != nil || event.Type == "" || name != "" && name != event.Type {
			return ErrInvalidResponse
		}
		event.Raw = json.RawMessage(payload)
		event.RequestID = requestID(resp)
		if event.Response != nil {
			event.Response.RequestID = event.RequestID
		}
		if err := onEvent(event); err != nil {
			return fmt.Errorf("llm: stream callback: %w", err)
		}
		switch event.Type {
		case "response.completed":
			return responseStatus(event.Response)
		case "response.failed", "response.incomplete":
			if event.Response == nil {
				return ErrInvalidResponse
			}
			return &ResponseStatusError{event.Response.ID, event.Response.Status, event.Response.Error, event.Response.IncompleteDetails}
		case "error":
			return &APIError{StatusCode: resp.StatusCode, RequestID: event.RequestID, Code: event.Code, Message: event.Message}
		}
	}
}
