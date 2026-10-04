// Package feishu implements Feishu custom bot webhook notifications.
package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	DefaultTimeout   = 5 * time.Second
	MaxMessageBytes  = 20 * 1024
	maxResponseBytes = 1024 * 1024
)

type Config struct {
	WebhookURL string
	Secret     string        // optional custom bot signing secret
	Timeout    time.Duration // zero means DefaultTimeout
	HTTPClient *http.Client  // optional transport; copied, not modified
}

// Client can be shared by concurrent senders. Every Send makes one attempt.
type Client struct {
	webhookURL string
	secret     string
	timeout    time.Duration
	httpClient *http.Client
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.WebhookURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, errors.New("feishu: webhook must be an absolute HTTP(S) URL without userinfo or fragment")
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("feishu: timeout must not be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	// A redirect must never forward the signed payload to a different endpoint.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{webhookURL: cfg.WebhookURL, secret: cfg.Secret, timeout: cfg.Timeout, httpClient: hc}, nil
}

// APIError represents a nonzero Feishu business status, including HTTP 200 errors.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu: API error %d: %s", e.Code, e.Message)
}

// HTTPError represents a non-2xx response. Response bodies are not included in errors.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("feishu: unexpected HTTP status %d", e.StatusCode)
}

// sign uses timestamp + newline + secret as the HMAC key and an empty message,
// as required by the Feishu custom bot signing protocol.
func sign(timestamp, secret string) string {
	h := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Send sends once, honoring both ctx and the configured timeout. A network error
// can occur after delivery; callers must account for duplicates before retrying.
func (c *Client) Send(ctx context.Context, message Message) error {
	if ctx == nil {
		return errors.New("feishu: context must not be nil")
	}
	if message.MsgType == "" {
		return errors.New("feishu: msg_type must not be empty")
	}
	payload := struct {
		Message
		Timestamp string `json:"timestamp,omitempty"`
		Sign      string `json:"sign,omitempty"`
	}{Message: message}
	if c.secret != "" {
		payload.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
		payload.Sign = sign(payload.Timestamp, c.secret)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: encode message: %w", err)
	}
	if len(body) > MaxMessageBytes {
		return fmt.Errorf("feishu: message exceeds %d bytes", MaxMessageBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("feishu: could not create webhook request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// net/http's url.Error includes the webhook token in its URL.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("feishu: send failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("feishu: read response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("feishu: response too large")
	}
	var result struct {
		Code          *int   `json:"code"`
		Msg           string `json:"msg"`
		StatusCode    *int   `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("feishu: decode response: %w", err)
	}
	if result.Code == nil && result.StatusCode == nil {
		return errors.New("feishu: response missing status code")
	}
	if result.Code != nil && *result.Code != 0 {
		return &APIError{Code: *result.Code, Message: result.Msg}
	}
	if result.StatusCode != nil && *result.StatusCode != 0 {
		return &APIError{Code: *result.StatusCode, Message: result.StatusMessage}
	}
	return nil
}

func (c *Client) SendText(ctx context.Context, text string) error {
	return c.Send(ctx, Text(text))
}

func (c *Client) SendMarkdown(ctx context.Context, title, content string) error {
	return c.Send(ctx, Markdown(title, content))
}
