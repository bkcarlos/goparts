// Package attachment uploads Feishu chat files, Drive files and document media.
// Authentication is supplied by the caller, for example user.Client.AccessToken
// or card.Client.AccessToken. No token is stored or logged by this package.
package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://open.feishu.cn/open-apis"
const DefaultTimeout = 2 * time.Minute
const MaxChatFileBytes int64 = 30 * 1024 * 1024
const MaxDriveFileBytes int64 = 20 * 1024 * 1024
const DefaultMaxFileBytes = MaxChatFileBytes
const DefaultMaxResponseBytes int64 = 2 * 1024 * 1024
const ScopeUploadChat = "im:resource:upload"
const ScopeUploadDrive = "drive:file:upload"
const ScopeUploadMedia = "docs:document.media:upload"

var ErrFileTooLarge = errors.New("feishu/attachment: file exceeds size limit")
var ErrSizeMismatch = errors.New("feishu/attachment: reader length differs from declared file size")
var ErrInvalidResponse = errors.New("feishu/attachment: invalid response")

type Config struct {
	TokenProvider    func(context.Context) (string, error)
	BaseURL          string
	Timeout          time.Duration // per operation, including token acquisition; zero uses two minutes
	MaxFileBytes     int64         // zero uses 30 MiB; individual endpoints impose lower platform limits
	MaxResponseBytes int64         // zero uses 2 MiB
	HTTPClient       *http.Client  // copied; redirects disabled, existing timeout retained
}

type Client struct {
	token                          func(context.Context) (string, error)
	baseURL                        string
	timeout                        time.Duration
	maxFileBytes, maxResponseBytes int64
	http                           *http.Client
	mediaGate                      chan struct{} // serialize media uploads for one client (platform constraint)
}

func New(cfg Config) (*Client, error) {
	if cfg.TokenProvider == nil {
		return nil, errors.New("feishu/attachment: TokenProvider is required")
	}
	if cfg.Timeout < 0 || cfg.MaxFileBytes < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == math.MaxInt64 {
		return nil, errors.New("feishu/attachment: invalid timeout or size limit")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxFileBytes == 0 {
		cfg.MaxFileBytes = DefaultMaxFileBytes
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("feishu/attachment: invalid base URL")
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{token: cfg.TokenProvider, baseURL: strings.TrimRight(cfg.BaseURL, "/"), timeout: cfg.Timeout, maxFileBytes: cfg.MaxFileBytes, maxResponseBytes: cfg.MaxResponseBytes, http: hc, mediaGate: make(chan struct{}, 1)}, nil
}

type APIError struct {
	StatusCode, Code   int
	Message, RequestID string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu/attachment: API failed (HTTP %d, code %d)", e.StatusCode, e.Code)
}
func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.attachment.api_error", "Feishu attachment request failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{"http_status": strconv.Itoa(e.StatusCode), "upstream_code": strconv.Itoa(e.Code), "request_id": e.RequestID}
}

func (c *Client) context(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, errors.New("feishu/attachment: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	return bounded, cancel, nil
}

func (c *Client) request(ctx context.Context, path, contentType string, body io.Reader, length int64, output any) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("feishu/attachment: token provider returned an empty token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return errors.New("feishu/attachment: invalid request")
	}
	req.ContentLength = length
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("feishu/attachment: HTTP request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("feishu/attachment: read response: %w", err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return errors.New("feishu/attachment: response exceeds size limit")
	}
	var envelope struct {
		Code *int            `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decodeErr := json.Unmarshal(data, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.Code != nil && *envelope.Code != 0) {
		e := &APIError{StatusCode: resp.StatusCode, Message: envelope.Msg, RequestID: resp.Header.Get("X-Tt-Logid")}
		if envelope.Code != nil {
			e.Code = *envelope.Code
		}
		return e
	}
	if decodeErr != nil || envelope.Code == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, output) != nil {
		return ErrInvalidResponse
	}
	return nil
}
