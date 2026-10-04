// Package user authorizes a Feishu application to act as a consenting user.
// It implements a small device OAuth flow and Docx client without the Lark CLI.
package user

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
const DefaultAccountsURL = "https://accounts.feishu.cn"
const ScopeReadDocuments = "docx:document:readonly"
const ScopeWriteDocuments = "docx:document"
const responseLimit = 8 * 1024 * 1024

var ErrLoginRequired = errors.New("feishu/user: user login required")
var ErrInvalidResponse = errors.New("feishu/user: invalid response")
var ErrDeviceExpired = errors.New("feishu/user: device authorization expired")
var ErrUnsupportedToken = errors.New("feishu/user: only Bearer tokens are supported")

type Config struct {
	OnPollTick       func(context.Context, PollTick) error
	RefreshLocker    Locker
	MaxResponseBytes int64 // zero retains the package default
	AppID            string
	AppSecret        string
	Scopes           []string      // explicit document scopes; offline_access is added automatically
	BaseURL          string        // defaults to DefaultBaseURL, includes /open-apis
	AccountsURL      string        // defaults to DefaultAccountsURL
	Timeout          time.Duration // per HTTP request, default 15s; login has its own deadline
	Store            TokenStore    // nil uses memory; one store/account per Client
	HTTPClient       *http.Client  // copied; redirects disabled
}

// Client is safe for concurrent document calls. Reuse a single Client for one
// stored account, so rotating refresh tokens cannot race between callers.
type Client struct {
	onPollTick                             func(context.Context, PollTick) error
	refreshLocker                          Locker
	maxResponseBytes                       int64
	appID, appSecret, baseURL, accountsURL string
	scopes                                 []string
	timeout                                time.Duration
	http                                   *http.Client
	store                                  TokenStore
	gate                                   chan struct{}
	pending                                *Token // a rotated token retained when persistence fails
	now                                    func() time.Time
	wait                                   func(context.Context, time.Duration) error
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AppID) == "" || cfg.AppSecret == "" {
		return nil, errors.New("feishu/user: AppID and AppSecret are required")
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("feishu: invalid response limit")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = responseLimit
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("feishu/user: timeout must not be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.AccountsURL == "" {
		cfg.AccountsURL = DefaultAccountsURL
	}
	for _, endpoint := range []string{cfg.BaseURL, cfg.AccountsURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return nil, errors.New("feishu/user: endpoints must be absolute HTTP(S) URLs without credentials, query or fragment")
		}
	}
	scopes := normalizeScopes(cfg.Scopes)
	if len(scopes) == 0 {
		return nil, errors.New("feishu/user: explicitly configure at least one scope")
	}
	scopes = normalizeScopes(append(scopes, "offline_access"))
	if cfg.Store == nil {
		cfg.Store = &MemoryStore{}
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{onPollTick: cfg.OnPollTick, refreshLocker: cfg.RefreshLocker, maxResponseBytes: cfg.MaxResponseBytes, appID: cfg.AppID, appSecret: cfg.AppSecret, baseURL: strings.TrimRight(cfg.BaseURL, "/"), accountsURL: strings.TrimRight(cfg.AccountsURL, "/"), scopes: scopes, timeout: cfg.Timeout, http: hc, store: cfg.Store, gate: make(chan struct{}, 1), now: time.Now, wait: waitContext}, nil
}

type APIError struct {
	StatusCode int
	Code       int
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu/user: API failed (HTTP %d, code %d)", e.StatusCode, e.Code)
}

type OAuthError struct {
	StatusCode  int
	Code        int
	ErrorCode   string
	Description string
}

func (e *OAuthError) Error() string {
	return fmt.Sprintf("feishu/user: OAuth failed (HTTP %d, code %d, error %s)", e.StatusCode, e.Code, e.ErrorCode)
}

type ScopeError struct{ Missing []string }

func (e *ScopeError) Error() string {
	return "feishu/user: missing granted scopes: " + strings.Join(e.Missing, ", ")
}

func (c *Client) lock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("feishu/user: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Client) unlock() { <-c.gate }

func (c *Client) send(ctx context.Context, method, endpoint, contentType string, body []byte, auth string) ([]byte, int, http.Header, error) {
	if ctx == nil {
		return nil, 0, nil, errors.New("feishu/user: context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, errors.New("feishu/user: invalid request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, 0, nil, fmt.Errorf("feishu/user: HTTP request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("feishu/user: read response: %w", err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return nil, resp.StatusCode, resp.Header, errors.New("feishu/user: response exceeds size limit")
	}
	return data, resp.StatusCode, resp.Header, nil
}

func (c *Client) apiWithToken(ctx context.Context, accessToken, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return fmt.Errorf("feishu/user: encode request: %w", err)
		}
	}
	data, status, headers, err := c.send(ctx, method, c.baseURL+path, "application/json; charset=utf-8", body, "Bearer "+accessToken)
	if err != nil {
		return err
	}
	var envelope struct {
		Code *int            `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decodeErr := json.Unmarshal(data, &envelope)
	if status < 200 || status >= 300 || (envelope.Code != nil && *envelope.Code != 0) {
		e := &APIError{StatusCode: status, Message: envelope.Msg, RequestID: headers.Get("X-Tt-Logid")}
		if envelope.Code != nil {
			e.Code = *envelope.Code
		}
		return e
	}
	if decodeErr != nil || envelope.Code == nil {
		return ErrInvalidResponse
	}
	if output != nil {
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, output) != nil {
			return ErrInvalidResponse
		}
	}
	return nil
}

func (c *Client) api(ctx context.Context, method, path string, input, output any) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	// Never fall back to a tenant token and never replay document mutations.
	return c.apiWithToken(ctx, token, method, path, input, output)
}

func normalizeScopes(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		for _, scope := range strings.Fields(strings.ReplaceAll(value, ",", " ")) {
			if !seen[scope] {
				seen[scope] = true
				result = append(result, scope)
			}
		}
	}
	return result
}

func (c *Client) checkScopes(scope string) error {
	granted := map[string]bool{}
	for _, s := range strings.Fields(scope) {
		granted[s] = true
	}
	var missing []string
	for _, s := range c.scopes {
		if !granted[s] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return &ScopeError{Missing: missing}
	}
	return nil
}

func waitContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
