// Package openapi shares bounded JSON transport between Feishu business clients.
package openapi

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
	"regexp"
	"strings"
	"time"
)

type Config struct {
	TokenProvider    func(context.Context) (string, error)
	BaseURL          string
	Timeout          time.Duration
	MaxResponseBytes int64
	HTTPClient       *http.Client
}
type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) (*Client, error) {
	if cfg.TokenProvider == nil || cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == math.MaxInt64 {
		return nil, errors.New("feishu/openapi: invalid config")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://open.feishu.cn/open-apis"
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return nil, errors.New("feishu/openapi: invalid base URL")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 4 << 20
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{cfg, hc}, nil
}

type APIError struct {
	StatusCode, Code   int
	Message, RequestID string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu/openapi: request failed (HTTP %d, code %d)", e.StatusCode, e.Code)
}
func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.openapi.error", "Feishu business API request failed"
}

var ErrInvalidResponse = errors.New("feishu/openapi: invalid response")
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidID(id string) bool { return len(id) > 0 && len(id) <= 512 && idPattern.MatchString(id) }

// Do is one attempt. Paths must be relative to BaseURL; credentials are never
// sent to redirects. Writes are not retried because acceptance may be ambiguous.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, input, output any) error {
	if ctx == nil || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") || strings.Contains(path, "..") {
		return errors.New("feishu/openapi: invalid context/path")
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	token, err := c.cfg.TokenProvider(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("feishu/openapi: empty access token")
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	endpoint := c.cfg.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("feishu/openapi: invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > c.cfg.MaxResponseBytes {
		return errors.New("feishu/openapi: response exceeds limit")
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	parseErr := json.Unmarshal(b, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != nil && *envelope.Code != 0 {
		e := &APIError{StatusCode: resp.StatusCode, Message: envelope.Message, RequestID: resp.Header.Get("X-Tt-Logid")}
		if envelope.Code != nil {
			e.Code = *envelope.Code
		}
		return e
	}
	if parseErr != nil || envelope.Code == nil {
		return ErrInvalidResponse
	}
	if output != nil && (len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, output) != nil) {
		return ErrInvalidResponse
	}
	return nil
}
