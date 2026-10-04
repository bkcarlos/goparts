package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const DefaultBaseURL = "https://open.feishu.cn"
const DefaultMaxFrameBytes int64 = 4 * 1024 * 1024
const DefaultMaxEventBytes int64 = 8 * 1024 * 1024
const DefaultMaxResponseBytes int64 = 1024 * 1024

var ErrAlreadyRunning = errors.New("feishu/events: client already running")

type Config struct {
	AppID, AppSecret                               string
	Dispatcher                                     *Dispatcher
	BaseURL                                        string // origin, WITHOUT /open-apis
	HTTPClient                                     *http.Client
	Dialer                                         *websocket.Dialer // copied, including its TLS config
	ConnectTimeout                                 time.Duration     // default 10s for bootstrap and dial, each
	WriteTimeout                                   time.Duration     // default 5s
	HandlerTimeout                                 time.Duration     // default 2.5s; handlers must obey context
	PingInterval                                   time.Duration     // zero uses server configuration, fallback 120s
	ReconnectInterval                              time.Duration     // zero uses server configuration, fallback 120s
	DisableReconnect                               bool
	MaxReconnectAttempts                           int // zero has no local cap; server's cap still applies
	MaxFrameBytes, MaxEventBytes, MaxResponseBytes int64
	QueueSize                                      int                          // default 64; handlers run serially
	OnConnected                                    func(context.Context)        // called for every successful handshake
	OnError                                        func(context.Context, error) // transport/handler errors, synchronous
}
type Client struct {
	cfg     Config
	http    *http.Client
	dialer  websocket.Dialer
	running atomic.Bool
}

// APIError includes bootstrap/handshake diagnostics. Error and reporting fields
// deliberately exclude the service message and the credential-bearing WS URL.
type APIError struct {
	StatusCode, Code   int
	Message, RequestID string
	Retryable          bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu/events: connection API failed (HTTP %d, code %d)", e.StatusCode, e.Code)
}
func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.events.api_error", "Feishu event connection failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{"http_status": strconv.Itoa(e.StatusCode), "upstream_code": strconv.Itoa(e.Code), "request_id": e.RequestID}
}

// TransportError preserves errors.Is/As without including secret URLs in text.
type TransportError struct {
	Operation string
	cause     error
}

func (e *TransportError) Error() string { return "feishu/events: " + e.Operation + " failed" }
func (e *TransportError) Unwrap() error { return e.cause }

type serverConfig struct {
	ReconnectCount    *int `json:"ReconnectCount"`
	ReconnectInterval *int `json:"ReconnectInterval"`
	ReconnectNonce    *int `json:"ReconnectNonce"`
	PingInterval      *int `json:"PingInterval"`
}

func mergeConfig(old, next serverConfig) (serverConfig, error) {
	if next.ReconnectCount != nil {
		if *next.ReconnectCount < -1 {
			return old, errProtocol
		}
		old.ReconnectCount = next.ReconnectCount
	}
	for _, v := range []*int{next.ReconnectInterval, next.ReconnectNonce, next.PingInterval} {
		if v != nil && (*v < 0 || *v > 86400) {
			return old, errProtocol
		}
	}
	if next.ReconnectInterval != nil {
		old.ReconnectInterval = next.ReconnectInterval
	}
	if next.ReconnectNonce != nil {
		old.ReconnectNonce = next.ReconnectNonce
	}
	if next.PingInterval != nil {
		old.PingInterval = next.PingInterval
	}
	return old, nil
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AppID) == "" || cfg.AppSecret == "" || cfg.Dispatcher == nil {
		return nil, errors.New("feishu/events: AppID, AppSecret and Dispatcher are required")
	}
	if cfg.ConnectTimeout < 0 || cfg.WriteTimeout < 0 || cfg.HandlerTimeout < 0 || cfg.PingInterval < 0 || cfg.PingInterval > 24*time.Hour || cfg.ReconnectInterval < 0 || cfg.ReconnectInterval > 24*time.Hour || cfg.MaxReconnectAttempts < 0 || cfg.QueueSize < 0 {
		return nil, errors.New("feishu/events: invalid duration or queue/reconnect limit")
	}
	for _, v := range []int64{cfg.MaxFrameBytes, cfg.MaxEventBytes, cfg.MaxResponseBytes} {
		if v < 0 || v > math.MaxInt32 {
			return nil, errors.New("feishu/events: invalid size limit")
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("feishu/events: BaseURL must be an HTTP(S) origin")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.HandlerTimeout == 0 {
		cfg.HandlerTimeout = 2500 * time.Millisecond
	}
	if cfg.MaxFrameBytes == 0 {
		cfg.MaxFrameBytes = DefaultMaxFrameBytes
	}
	if cfg.MaxEventBytes == 0 {
		cfg.MaxEventBytes = DefaultMaxEventBytes
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 64
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dialer := *websocket.DefaultDialer
	if cfg.Dialer != nil {
		dialer = *cfg.Dialer
	}
	if dialer.TLSClientConfig != nil {
		dialer.TLSClientConfig = dialer.TLSClientConfig.Clone()
	}
	dialer.HandshakeTimeout = cfg.ConnectTimeout
	return &Client{cfg: cfg, http: hc, dialer: dialer}, nil
}

// Run blocks until cancellation or a permanent/exhausted connection error.
// One Run per client; cancellation closes the socket and joins owned workers.
func (c *Client) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("feishu/events: context is required")
	}
	if !c.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer c.running.Store(false)
	conf := serverConfig{}
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, service, next, err := c.connect(ctx)
		if next != nil {
			conf = *next
		}
		if err == nil {
			attempts = 0
			conf, err = c.session(ctx, conn, service, conf)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.report(ctx, err)
		var api *APIError
		if c.cfg.DisableReconnect || errors.Is(err, errProtocol) || errors.As(err, &api) && !api.Retryable {
			return err
		}
		limit := c.cfg.MaxReconnectAttempts
		if conf.ReconnectCount != nil && *conf.ReconnectCount >= 0 {
			if *conf.ReconnectCount == 0 {
				return err
			}
			if limit == 0 || *conf.ReconnectCount < limit {
				limit = *conf.ReconnectCount
			}
		}
		if limit > 0 && attempts >= limit {
			return err
		}
		delay := c.cfg.ReconnectInterval
		if delay == 0 {
			delay = 120 * time.Second
			if conf.ReconnectInterval != nil && *conf.ReconnectInterval > 0 {
				delay = time.Duration(*conf.ReconnectInterval) * time.Second
			}
		}
		if attempts == 0 && conf.ReconnectNonce != nil && *conf.ReconnectNonce > 0 {
			delay += time.Duration(rand.Int63n(int64(time.Duration(*conf.ReconnectNonce) * time.Second)))
		}
		attempts++
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (c *Client) report(ctx context.Context, err error) {
	if err == nil || c.cfg.OnError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.OnError(ctx, err)
}

func (c *Client) connect(ctx context.Context) (*websocket.Conn, uint64, *serverConfig, error) {
	endpoint, conf, err := c.bootstrap(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "wss" && u.Scheme != "ws") || u.User != nil || u.Fragment != "" {
		return nil, 0, &conf, errProtocol
	}
	// HTTPS bootstrap must never downgrade credentials to a plain socket.
	if strings.HasPrefix(c.cfg.BaseURL, "https:") && u.Scheme != "wss" {
		return nil, 0, &conf, errProtocol
	}
	service, err := strconv.ParseUint(u.Query().Get("service_id"), 10, 31)
	if err != nil {
		return nil, 0, &conf, errProtocol
	}
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
	defer cancel()
	conn, resp, err := c.dialer.DialContext(dialCtx, endpoint, nil)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		if resp != nil {
			if resp.Body != nil {
				resp.Body.Close()
			}
			code, _ := strconv.Atoi(resp.Header.Get("Handshake-Status"))
			authCode, _ := strconv.Atoi(resp.Header.Get("Handshake-Autherrcode"))
			return nil, 0, &conf, &APIError{StatusCode: resp.StatusCode, Code: code, Message: resp.Header.Get("Handshake-Msg"), RequestID: resp.Header.Get("X-Tt-Logid"), Retryable: code != 403 && authCode != 1000040350 && resp.StatusCode != 401 && resp.StatusCode != 403}
		}
		return nil, 0, &conf, &TransportError{Operation: "websocket dial", cause: err}
	}
	return conn, service, &conf, nil
}
func (c *Client) bootstrap(ctx context.Context) (string, serverConfig, error) {
	conf := serverConfig{}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"AppID": c.cfg.AppID, "AppSecret": c.cfg.AppSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/callback/ws/endpoint", bytes.NewReader(body))
	if err != nil {
		return "", conf, errProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", conf, &TransportError{Operation: "endpoint discovery", cause: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxResponseBytes+1))
	if err != nil {
		return "", conf, &TransportError{Operation: "endpoint response", cause: err}
	}
	if int64(len(b)) > c.cfg.MaxResponseBytes {
		return "", conf, errProtocol
	}
	var result struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
		Data *struct {
			URL    string       `json:"URL"`
			Config serverConfig `json:"ClientConfig"`
		} `json:"data"`
	}
	decodeErr := json.Unmarshal(b, &result)
	if resp.StatusCode != 200 || result.Code != nil && *result.Code != 0 {
		code := 0
		if result.Code != nil {
			code = *result.Code
		}
		return "", conf, &APIError{StatusCode: resp.StatusCode, Code: code, Message: result.Msg, RequestID: resp.Header.Get("X-Tt-Logid"), Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500 || resp.StatusCode == 200 && (code == 1 || code == 1000040343)}
	}
	if decodeErr != nil || result.Code == nil || result.Data == nil || result.Data.URL == "" {
		return "", conf, errProtocol
	}
	conf, err = mergeConfig(conf, result.Data.Config)
	return result.Data.URL, conf, err
}
