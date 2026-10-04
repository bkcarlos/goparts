// Package artifact provides a configurable artifact-catalog client. Its documented
// JSON protocol is provider-neutral; it does not claim proprietary BOS compatibility.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type AppPackage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	CommitID    string `json:"commit_id"`
	URL         string `json:"url"`
	InternalURL string `json:"internal_url,omitempty"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}
type Page struct {
	Items      []AppPackage `json:"items"`
	NextCursor string       `json:"next_cursor"`
}
type Query struct {
	CommitID, Cursor string
	Limit            int
}
type Backend interface {
	GetPackageInfo(context.Context, string) (AppPackage, error)
	ListPackages(context.Context, Query) (Page, error)
}
type Config struct {
	BaseURL          string
	Headers          http.Header
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
}
type HTTPBackend struct {
	cfg  Config
	http *http.Client
}

func validURL(text string) bool {
	u, err := url.Parse(text)
	return err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Fragment == ""
}
func NewHTTPBackend(cfg Config) (*HTTPBackend, error) {
	if !validURL(cfg.BaseURL) || cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes > 64<<20 {
		return nil, errors.New("artifact: invalid HTTP config")
	}
	u, _ := url.Parse(cfg.BaseURL)
	if u.RawQuery != "" || u.ForceQuery {
		return nil, errors.New("artifact: base URL must not contain query")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 4 << 20
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.Headers = cfg.Headers.Clone()
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPBackend{cfg, hc}, nil
}
func (b *HTTPBackend) get(ctx context.Context, path string, query url.Values, out any) error {
	if ctx == nil {
		return errors.New("artifact: context required")
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	defer cancel()
	endpoint := b.cfg.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return errors.New("artifact: invalid request")
	}
	req.Header = b.cfg.Headers.Clone()
	resp, err := b.http.Do(req)
	if err != nil {
		return errors.New("artifact: catalog request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, b.cfg.MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > b.cfg.MaxResponseBytes {
		return errors.New("artifact: response exceeds limit")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("artifact: invalid catalog response")
	}
	return nil
}

type StatusError struct{ StatusCode int }

func (e *StatusError) Error() string { return "artifact: catalog returned unsuccessful status" }
func (b *HTTPBackend) GetPackageInfo(ctx context.Context, id string) (AppPackage, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		return AppPackage{}, errors.New("artifact: invalid package ID")
	}
	var out AppPackage
	err := b.get(ctx, "/packages/"+url.PathEscape(id), nil, &out)
	if err == nil && out.ID != id {
		err = errors.New("artifact: package ID mismatch")
	}
	return out, err
}
func (b *HTTPBackend) ListPackages(ctx context.Context, q Query) (Page, error) {
	if q.Limit < 0 || q.Limit > 1000 {
		return Page{}, errors.New("artifact: invalid page size")
	}
	values := url.Values{}
	if q.CommitID != "" {
		values.Set("commit_id", q.CommitID)
	}
	if q.Cursor != "" {
		values.Set("cursor", q.Cursor)
	}
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	var out Page
	err := b.get(ctx, "/packages", values, &out)
	return out, err
}

type PackageManager struct {
	Backend           Backend
	HTTPClient        *http.Client
	MaxBytes          int64
	Timeout           time.Duration
	InternalAvailable func(context.Context, string) bool
}

func (m *PackageManager) GetPackageInfo(ctx context.Context, id string) (AppPackage, error) {
	if m.Backend == nil {
		return AppPackage{}, errors.New("artifact: backend required")
	}
	return m.Backend.GetPackageInfo(ctx, id)
}
func (m *PackageManager) ListPackages(ctx context.Context, q Query) (Page, error) {
	if m.Backend == nil {
		return Page{}, errors.New("artifact: backend required")
	}
	return m.Backend.ListPackages(ctx, q)
}
func (m *PackageManager) QueryWithCommitID(ctx context.Context, commit, cursor string, limit int) (Page, error) {
	if commit == "" {
		return Page{}, errors.New("artifact: commit ID required")
	}
	return m.ListPackages(ctx, Query{commit, cursor, limit})
}

// InternalAvailable is an explicit user-supplied probe; there is no network scan
// or automatic host rewriting. Catalog credentials are never copied to downloads.
func (m *PackageManager) DownloadURL(ctx context.Context, p AppPackage) (string, error) {
	if p.InternalURL != "" && validURL(p.InternalURL) && m.InternalAvailable != nil && m.InternalAvailable(ctx, p.InternalURL) {
		return p.InternalURL, nil
	}
	if !validURL(p.URL) {
		return "", errors.New("artifact: invalid download URL")
	}
	return p.URL, nil
}
func (m *PackageManager) DownloadPackage(ctx context.Context, p AppPackage, destination string) error {
	if ctx == nil || destination == "" || m.MaxBytes < 0 || m.MaxBytes > 1<<50 || m.Timeout < 0 || p.Size < 0 {
		return errors.New("artifact: invalid download config")
	}
	digest, err := hex.DecodeString(p.SHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("artifact: SHA-256 required")
	}
	limit := m.MaxBytes
	if limit == 0 {
		limit = 10 << 30
	}
	if p.Size > limit {
		return errors.New("artifact: package exceeds size limit")
	}
	timeout := m.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	endpoint, err := m.DownloadURL(ctx, p)
	if err != nil {
		return err
	}
	hc := &http.Client{}
	if m.HTTPClient != nil {
		*hc = *m.HTTPClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := hc.Do(req)
	if err != nil {
		return errors.New("artifact: download request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &StatusError{resp.StatusCode}
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return errors.New("artifact: encoded download rejected")
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, p.Size+1))
	if err != nil {
		return err
	}
	if n != p.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), p.SHA256) {
		return errors.New("artifact: size/checksum mismatch")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmp.Name(), destination)
}
