package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.openai.com/v1"
const DefaultTimeout = 2 * time.Minute
const DefaultMaxResponseBytes int64 = 8 * 1024 * 1024
const DefaultMaxEventBytes = 1024 * 1024

var ErrResponseTooLarge = errors.New("llm: response exceeds size limit")
var ErrEventTooLarge = errors.New("llm: SSE event exceeds size limit")
var ErrInvalidResponse = errors.New("llm: invalid API response")

type Config struct {
	BaseURL          string        // API root, including /v1 or the provider's compatible path
	APIKey           string        // optional for unauthenticated local endpoints
	Model            string        // default chat model; no model is selected automatically
	Timeout          time.Duration // entire request/stream; zero defaults to two minutes
	MaxResponseBytes int64
	MaxEventBytes    int
	Headers          http.Header
	HTTPClient       *http.Client // copied; redirects disabled, existing client timeout retained
}

// Client may be shared concurrently. Requests/maps must not be mutated while in use.
type Client struct {
	baseURL          string
	apiKey           string
	model            string
	timeout          time.Duration
	maxResponseBytes int64
	maxEventBytes    int
	headers          http.Header
	http             *http.Client
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("llm: BaseURL must be an absolute HTTP(S) API root without userinfo, query or fragment")
	}
	if cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == math.MaxInt64 || cfg.MaxEventBytes < 0 || cfg.MaxEventBytes == math.MaxInt {
		return nil, errors.New("llm: invalid timeout or size limit")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if cfg.MaxEventBytes == 0 {
		cfg.MaxEventBytes = DefaultMaxEventBytes
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	headers := make(http.Header)
	for key, values := range cfg.Headers {
		headers[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return &Client{baseURL: strings.TrimRight(u.String(), "/"), apiKey: cfg.APIKey, model: cfg.Model, timeout: cfg.Timeout, maxResponseBytes: cfg.MaxResponseBytes, maxEventBytes: cfg.MaxEventBytes, headers: headers, http: hc}, nil
}

// APIError exposes provider diagnostics without including them in Error(), since
// provider messages can contain prompts or credentials. RequestID aids tracing.
type APIError struct {
	StatusCode int
	RequestID  string
	Type       string
	Code       string
	Message    string
	RetryAfter string // original HTTP Retry-After value; no automatic retry
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: API request failed (HTTP %d)", e.StatusCode)
}

func (c *Client) Chat(ctx context.Context, input ChatRequest) (*ChatResponse, error) {
	body, err := c.chatBody(input, false)
	if err != nil {
		return nil, err
	}
	var result ChatResponse
	id, err := c.postJSON(ctx, "/chat/completions", body, &result)
	if err != nil {
		return nil, err
	}
	if len(result.Choices) == 0 {
		return nil, ErrInvalidResponse
	}
	for _, choice := range result.Choices {
		if choice.Message.Role != RoleAssistant {
			return nil, ErrInvalidResponse
		}
	}
	result.RequestID = id
	return &result, nil
}

func (c *Client) Embeddings(ctx context.Context, input EmbeddingRequest) (*EmbeddingResponse, error) {
	if strings.TrimSpace(input.Model) == "" || len(input.Input) == 0 {
		return nil, errors.New("llm: embedding model and input are required")
	}
	for _, text := range input.Input {
		if text == "" {
			return nil, errors.New("llm: embedding input must not be empty")
		}
	}
	if input.Dimensions != nil && *input.Dimensions <= 0 {
		return nil, errors.New("llm: dimensions must be positive")
	}
	body, err := encode(input, input.Extra, map[string]any{"encoding_format": "float"})
	if err != nil {
		return nil, err
	}
	var result EmbeddingResponse
	id, err := c.postJSON(ctx, "/embeddings", body, &result)
	if err != nil {
		return nil, err
	}
	if len(result.Data) != len(input.Input) {
		return nil, ErrInvalidResponse
	}
	seen := make(map[int]bool, len(result.Data))
	dimensions := len(result.Data[0].Embedding)
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(input.Input) || seen[item.Index] || len(item.Embedding) == 0 || len(item.Embedding) != dimensions || (input.Dimensions != nil && len(item.Embedding) != *input.Dimensions) {
			return nil, ErrInvalidResponse
		}
		seen[item.Index] = true
	}
	result.RequestID = id
	return &result, nil
}

func (c *Client) chatBody(input ChatRequest, stream bool) ([]byte, error) {
	if input.Model == "" {
		input.Model = c.model
	}
	if strings.TrimSpace(input.Model) == "" || len(input.Messages) == 0 {
		return nil, errors.New("llm: model and messages are required")
	}
	for _, message := range input.Messages {
		switch message.Role {
		case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant:
		case RoleTool:
			if message.ToolCallID == "" {
				return nil, errors.New("llm: tool result requires tool_call_id")
			}
		default:
			return nil, errors.New("llm: unsupported message role")
		}
	}
	if input.MaxTokens != nil && input.MaxCompletionTokens != nil {
		return nil, errors.New("llm: set only one token limit")
	}
	for _, n := range []*int{input.MaxTokens, input.MaxCompletionTokens} {
		if n != nil && *n <= 0 {
			return nil, errors.New("llm: token limit must be positive")
		}
	}
	if input.Temperature != nil && (math.IsNaN(*input.Temperature) || *input.Temperature < 0 || *input.Temperature > 2) {
		return nil, errors.New("llm: temperature must be in [0,2]")
	}
	if input.TopP != nil && (math.IsNaN(*input.TopP) || *input.TopP < 0 || *input.TopP > 1) {
		return nil, errors.New("llm: top_p must be in [0,1]")
	}
	for _, tool := range input.Tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			return nil, errors.New("llm: tools must define named functions")
		}
	}
	fields := map[string]any{"stream": stream, "stream_options": nil}
	if stream && input.IncludeUsage {
		fields["stream_options"] = map[string]bool{"include_usage": true}
	}
	return encode(input, input.Extra, fields)
}

// encode keeps extension fields separate from known fields even when omitted.
func encode(input any, extra map[string]any, controlled map[string]any) ([]byte, error) {
	reserved := make(map[string]bool)
	t := reflect.TypeOf(input)
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			reserved[tag] = true
		}
	}
	for key := range controlled {
		reserved[key] = true
	}
	for key := range extra {
		if reserved[key] {
			return nil, fmt.Errorf("llm: extension cannot override %q", key)
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("llm: encode extension: %w", err)
		}
		fields[key] = data
	}
	for key, value := range controlled {
		if value != nil {
			data, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			fields[key] = data
		}
	}
	return json.Marshal(fields)
}

func (c *Client) request(ctx context.Context, path string, body []byte, stream bool) (*http.Response, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, errors.New("llm: context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, errors.New("llm: invalid request")
	}
	req.Header = c.headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("llm: send request: %w", withoutURL(err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer cancel()
		defer resp.Body.Close()
		data, err := readBounded(resp.Body, c.maxResponseBytes)
		if err != nil {
			return nil, nil, err
		}
		apiErr := parseAPIError(data, resp)
		if apiErr == nil {
			apiErr = &APIError{StatusCode: resp.StatusCode, RequestID: requestID(resp), RetryAfter: resp.Header.Get("Retry-After")}
		}
		return nil, nil, apiErr
	}
	resp.Request = req
	return resp, cancel, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte, out any) (string, error) {
	resp, cancel, err := c.request(ctx, path, body, false)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, c.maxResponseBytes)
	if err != nil {
		return "", err
	}
	if apiErr := parseAPIError(data, resp); apiErr != nil {
		return "", apiErr
	}
	if err := json.Unmarshal(data, out); err != nil {
		return "", fmt.Errorf("%w: invalid JSON", ErrInvalidResponse)
	}
	return requestID(resp), nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", withoutURL(err))
	}
	if int64(len(data)) > limit {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}

func requestID(resp *http.Response) string {
	if id := resp.Header.Get("X-Request-Id"); id != "" {
		return id
	}
	return resp.Header.Get("Request-Id")
}

func parseAPIError(data []byte, resp *http.Response) *APIError {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Error) == 0 || string(envelope.Error) == "null" {
		return nil
	}
	err := &APIError{StatusCode: resp.StatusCode, RequestID: requestID(resp), RetryAfter: resp.Header.Get("Retry-After")}
	var detail struct {
		Message string          `json:"message"`
		Type    string          `json:"type"`
		Code    json.RawMessage `json:"code"`
	}
	if json.Unmarshal(envelope.Error, &detail) == nil {
		err.Message, err.Type = detail.Message, detail.Type
		if len(detail.Code) > 0 && string(detail.Code) != "null" {
			if json.Unmarshal(detail.Code, &err.Code) != nil {
				err.Code = string(detail.Code)
			}
		}
	} else {
		_ = json.Unmarshal(envelope.Error, &err.Message)
	}
	return err
}

func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// CloseIdleConnections also affects other clients when their transport is shared.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }
