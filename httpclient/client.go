// Package httpclient provides bounded HTTP requests and JSON helpers.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

const DefaultTimeout = 10 * time.Second
const DefaultMaxResponseBytes int64 = 4 * 1024 * 1024

var ErrResponseTooLarge = errors.New("httpclient: response exceeds size limit")

type Config struct {
	Hooks            Hooks
	ErrorBodyBytes   int                 // opt-in preview size; requires RedactBody
	RedactBody       func([]byte) []byte // called on a copy; output is bounded again
	Timeout          time.Duration       // zero defaults to 10s, including response body reads
	MaxResponseBytes int64               // zero defaults to 4 MiB
	Headers          http.Header         // copied at construction; request headers override these
	Transport        http.RoundTripper   // optional; must be safe for concurrent requests
}

// Client is safe for concurrent use. It does not follow redirects or implement
// application retries. The standard transport's connection retry behavior applies.
type Client struct {
	http             *http.Client
	headers          http.Header
	maxResponseBytes int64
	hooks            Hooks
	errorBodyBytes   int
	redactBody       func([]byte) []byte
}

type Request struct {
	Method  string // empty defaults to GET
	URL     string
	Headers http.Header
	Body    io.Reader // owned by net/http during the request; do not reuse concurrently
}

type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type StatusError struct {
	BodyPreview []byte // opt-in, never included in Error()
	StatusCode  int
	Method      string
	Headers     http.Header // independent copy; may contain sensitive server values
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpclient: unexpected HTTP status %d", e.StatusCode)
}

func New(cfg Config) (*Client, error) {
	if cfg.ErrorBodyBytes < 0 || cfg.ErrorBodyBytes > 0 && cfg.RedactBody == nil {
		return nil, errors.New("httpclient: body preview requires a redactor and nonnegative limit")
	}
	if cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("httpclient: invalid timeout or response size limit")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	transport := cfg.Transport
	if transport == nil {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	return &Client{
		http:             &http.Client{Timeout: cfg.Timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		headers:          cloneHeaders(cfg.Headers),
		maxResponseBytes: cfg.MaxResponseBytes,
		hooks:            cfg.Hooks, errorBodyBytes: cfg.ErrorBodyBytes, redactBody: cfg.RedactBody,
	}, nil
}

func cloneHeaders(source http.Header) http.Header {
	dst := make(http.Header, len(source))
	for key, values := range source {
		key = http.CanonicalHeaderKey(key)
		dst[key] = append(dst[key], values...)
	}
	return dst
}

// Do buffers a bounded response and always closes the response body. On non-2xx,
// it returns both the response and a *StatusError. On oversized/read failures,
// the returned response has metadata but no partial body.
func (c *Client) Do(ctx context.Context, input Request) (response *Response, resultErr error) {
	if ctx == nil {
		return nil, errors.New("httpclient: context is required")
	}
	u, err := url.Parse(input.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("httpclient: URL must be absolute HTTP(S) without userinfo")
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, input.Body)
	if err != nil {
		return nil, errors.New("httpclient: invalid request")
	}
	req.Header = c.headers.Clone()
	for key, values := range cloneHeaders(input.Headers) {
		req.Header[key] = values
	}
	start := time.Now()
	if c.hooks.OnRequest != nil {
		c.hooks.OnRequest(ctx, RequestEvent{Method: req.Method, Host: u.Host, Path: u.EscapedPath(), Headers: safeHeaders(req.Header)})
	}
	defer func() {
		if c.hooks.OnResponse != nil {
			event := ResponseEvent{Method: req.Method, Duration: time.Since(start), Failed: resultErr != nil}
			if response != nil {
				event.StatusCode = response.StatusCode
				event.Bytes = int64(len(response.Body))
				event.Headers = safeHeaders(response.Headers)
			}
			c.hooks.OnResponse(ctx, event)
		}
	}()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpclient: request failed: %w", withoutURL(err))
	}
	defer resp.Body.Close()
	result := &Response{StatusCode: resp.StatusCode, Headers: resp.Header.Clone()}
	var statusErr error
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr = &StatusError{StatusCode: resp.StatusCode, Method: req.Method, Headers: resp.Header.Clone()}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		readErr := fmt.Errorf("httpclient: read response: %w", withoutURL(err))
		if statusErr != nil {
			return result, errors.Join(statusErr, readErr)
		}
		return result, readErr
	}
	if int64(len(data)) > c.maxResponseBytes {
		if statusErr != nil {
			return result, errors.Join(statusErr, ErrResponseTooLarge)
		}
		return result, ErrResponseTooLarge
	}
	if statusErr != nil && c.errorBodyBytes > 0 {
		n := min(len(data), c.errorBodyBytes)
		preview := c.redactBody(append([]byte(nil), data[:n]...))
		statusErr.(*StatusError).BodyPreview = append([]byte(nil), preview[:min(len(preview), c.errorBodyBytes)]...)
	}
	result.Body = data
	return result, statusErr
}

// DoJSON encodes input when non-nil and decodes a successful nonempty response
// into output when non-nil. output must be a non-nil pointer. For per-request
// headers or streaming request bodies, use Do. Empty responses leave output unchanged.
func (c *Client) DoJSON(ctx context.Context, method, endpoint string, input, output any) (*Response, error) {
	if output != nil {
		v := reflect.ValueOf(output)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return nil, errors.New("httpclient: JSON output must be a non-nil pointer")
		}
	}
	req := Request{Method: method, URL: endpoint, Headers: make(http.Header)}
	req.Headers.Set("Accept", "application/json")
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("httpclient: encode JSON: %w", err)
		}
		req.Body = bytes.NewReader(data)
		req.Headers.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(ctx, req)
	if err != nil {
		return resp, err
	}
	if output != nil && len(bytes.TrimSpace(resp.Body)) != 0 {
		if err := json.Unmarshal(resp.Body, output); err != nil {
			return resp, fmt.Errorf("httpclient: decode JSON: %w", err)
		}
	}
	return resp, nil
}

// CloseIdleConnections releases idle pooled connections. An injected transport
// may be shared, so this can also affect other clients using that transport.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
