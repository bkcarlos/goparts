package card

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://open.feishu.cn/open-apis"
const responseLimit = 2 * 1024 * 1024

var ErrInvalidResponse = errors.New("feishu/card: invalid response")

type Config struct {
	TokenCache       TokenCache // nil retains per-client cache
	MaxResponseBytes int64      // zero retains the package default
	AppID            string
	AppSecret        string
	BaseURL          string        // includes /open-apis; defaults to DefaultBaseURL
	Timeout          time.Duration // per HTTP request, defaults to 15 seconds
	HTTPClient       *http.Client  // copied; redirects disabled
}

// Client supports self-built application bots using tenant_access_token.
// Reuse it across goroutines to share the in-memory token cache.
type Client struct {
	cache                     TokenCache
	maxResponseBytes          int64
	appID, appSecret, baseURL string
	timeout                   time.Duration
	http                      *http.Client
	gate                      chan struct{}
	token                     string
	expires                   time.Time
	now                       func() time.Time
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AppID) == "" || cfg.AppSecret == "" {
		return nil, errors.New("feishu/card: AppID and AppSecret are required")
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("feishu: invalid response limit")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = responseLimit
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("feishu/card: timeout must not be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("feishu/card: invalid base URL")
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{cache: cfg.TokenCache, maxResponseBytes: cfg.MaxResponseBytes, appID: cfg.AppID, appSecret: cfg.AppSecret, baseURL: strings.TrimRight(cfg.BaseURL, "/"), timeout: cfg.Timeout, http: hc, gate: make(chan struct{}, 1), now: time.Now}, nil
}

type APIError struct {
	StatusCode int
	Code       int
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu/card: API failed (HTTP %d, code %d)", e.StatusCode, e.Code)
}

// AccessToken returns a cached/refreshed application tenant token, for composing
// other Feishu clients such as attachment.Client. Do not log the returned token.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	return c.accessToken(ctx)
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("card: context required")
	}
	if c.cache != nil {
		token, err := c.cache.GetOrLoad(ctx, c.cacheKey(), func(ctx context.Context) (CachedToken, error) {
			value, err := c.localAccessToken(ctx)
			return CachedToken{Value: value, ExpiresAt: c.expires}, err
		})
		return token.Value, err
	}
	return c.localAccessToken(ctx)
}
func (c *Client) localAccessToken(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("feishu/card: context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.gate }()
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	start := c.now()
	data, err := c.request(ctx, "POST", "/auth/v3/tenant_access_token/internal", "", map[string]string{"app_id": c.appID, "app_secret": c.appSecret})
	if err != nil {
		return "", err
	}
	var result struct {
		Token  string `json:"tenant_access_token"`
		Expire int64  `json:"expire"`
	}
	if json.Unmarshal(data, &result) != nil || strings.TrimSpace(result.Token) == "" || result.Expire <= 0 || result.Expire > 86400 {
		return "", ErrInvalidResponse
	}
	lifetime := time.Duration(result.Expire) * time.Second
	skew := lifetime / 10
	if skew > 30*time.Second {
		skew = 30 * time.Second
	}
	c.token, c.expires = result.Token, start.Add(lifetime-skew)
	return c.token, nil
}

func (c *Client) request(ctx context.Context, method, path, token string, input any) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("feishu/card: context is required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("feishu/card: encode request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("feishu/card: invalid request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("feishu/card: HTTP request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("feishu/card: read response: %w", err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return nil, errors.New("feishu/card: response exceeds size limit")
	}
	var envelope struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeErr := json.Unmarshal(data, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.Code != nil && *envelope.Code != 0) {
		e := &APIError{StatusCode: resp.StatusCode, Message: envelope.Msg, RequestID: resp.Header.Get("X-Tt-Logid")}
		if envelope.Code != nil {
			e.Code = *envelope.Code
		}
		return nil, e
	}
	if decodeErr != nil || envelope.Code == nil {
		return nil, ErrInvalidResponse
	}
	return data, nil
}

func (c *Client) api(ctx context.Context, method, path string, input, output any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	// Single attempt: a timeout can occur after the mutation was accepted.
	data, err := c.request(ctx, method, path, token, input)
	if err != nil {
		return err
	}
	if output != nil {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(data, &envelope) != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, output) != nil {
			return ErrInvalidResponse
		}
	}
	return nil
}
