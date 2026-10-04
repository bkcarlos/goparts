package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type HTTPConfig struct {
	HTTPClient *http.Client
	Headers    http.Header
}
type HTTPSource struct {
	endpoint string
	client   *http.Client
	headers  http.Header
}

func NewHTTPSource(endpoint string, cfg HTTPConfig) (*HTTPSource, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, errors.New("download: invalid HTTP(S) URL")
	}
	client := &http.Client{}
	if cfg.HTTPClient != nil {
		*client = *cfg.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	h := cfg.Headers.Clone()
	if h == nil {
		h = make(http.Header)
	}
	for k := range h {
		if http.CanonicalHeaderKey(k) == "Range" || http.CanonicalHeaderKey(k) == "If-Range" {
			return nil, errors.New("download: HTTP source expects a complete response; range headers are unsupported")
		}
	}
	h.Set("Accept-Encoding", "identity")
	return &HTTPSource{endpoint: endpoint, client: client, headers: h}, nil
}
func (s *HTTPSource) Open(ctx context.Context) (Stream, error) {
	if ctx == nil {
		return Stream{}, errors.New("download: context required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return Stream{}, errors.New("download: invalid request")
	}
	req.Header = s.headers.Clone()
	resp, err := s.client.Do(req)
	if err != nil {
		return Stream{}, &HTTPError{Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return Stream{}, &HTTPError{StatusCode: resp.StatusCode}
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		resp.Body.Close()
		return Stream{}, errors.New("download: unexpected content encoding")
	}
	return Stream{Body: resp.Body, Size: resp.ContentLength}, nil
}

type HTTPError struct {
	StatusCode int
	Cause      error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("download: HTTP request failed (status %d)", e.StatusCode)
}
func (e *HTTPError) Unwrap() error { return e.Cause }
func (e *HTTPError) ErrorInfo() (string, string) {
	return "download.http_failed", "HTTP download failed"
}
func (e *HTTPError) ErrorFields() map[string]string {
	return map[string]string{"http_status": strconv.Itoa(e.StatusCode)}
}
