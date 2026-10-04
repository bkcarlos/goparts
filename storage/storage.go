// Package storage defines provider-neutral object storage operations.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("storage: object not found")
var ErrPermission = errors.New("storage: permission denied")
var ErrPrecondition = errors.New("storage: object changed or precondition failed")
var ErrUnsupported = errors.New("storage: operation is not supported")
var ErrSizeMismatch = errors.New("storage: source length differs from declared size")

type Object struct {
	Key                          string
	Size                         int64 // -1 when unknown; ETag is opaque, not necessarily an MD5 checksum
	ETag, ContentType, VersionID string
	LastModified                 time.Time
	Metadata                     map[string]string
}
type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	OnProgress  func(read, total int64) error
}
type GetOptions struct {
	Offset, Length int64
	IfMatch        string
} // Length=0 reads through EOF
type Reader struct {
	io.ReadCloser
	Object         Object
	Offset, Length int64
}
type ListOptions struct {
	Prefix, Cursor string
	Limit          int
}
type Page struct {
	Objects    []Object
	NextCursor string
}
type SignedURL struct {
	URL       string
	ExpiresAt time.Time
}

// Backend is the adapter contract. Keys are bucket-relative; Get returns an
// owned stream that callers must close. List returns one page, never all pages.
// Implementations must obey context, map common errors and be concurrency safe.
// Size is exact and nonnegative; Put must consume its input before returning.
type Backend interface {
	Put(context.Context, string, io.Reader, int64, PutOptions) (Object, error)
	Get(context.Context, string, GetOptions) (*Reader, error)
	Stat(context.Context, string) (Object, error)
	Delete(context.Context, string) error
	List(context.Context, ListOptions) (Page, error)
	PresignGet(context.Context, string, time.Duration) (SignedURL, error)
}

type Config struct {
	BasePath     string
	MaxReadBytes int64
} // default bounded ReadAll: 16 MiB
type Client struct {
	backend      Backend
	prefix       string
	maxReadBytes int64
}

func New(backend Backend, cfg Config) (*Client, error) {
	if backend == nil || cfg.MaxReadBytes < 0 || cfg.MaxReadBytes == math.MaxInt64 {
		return nil, errors.New("storage: backend and valid read limit are required")
	}
	base := strings.TrimSuffix(cfg.BasePath, "/")
	if cfg.BasePath != "" && (base == "" || !validKey(base) || strings.HasSuffix(base, "/")) {
		return nil, errors.New("storage: invalid base path")
	}
	if base != "" {
		base += "/"
	}
	if cfg.MaxReadBytes == 0 {
		cfg.MaxReadBytes = 16 * 1024 * 1024
	}
	return &Client{backend: backend, prefix: base, maxReadBytes: cfg.MaxReadBytes}, nil
}
func validKey(key string) bool {
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) || strings.ContainsAny(key, "\x00\r\n\\") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(key, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func (c *Client) key(ctx context.Context, key string) (string, error) {
	if ctx == nil {
		return "", errors.New("storage: context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validKey(key) {
		return "", errors.New("storage: invalid object key")
	}
	key = c.prefix + key
	if !validKey(key) || key == strings.TrimSuffix(c.prefix, "/") {
		return "", errors.New("storage: invalid object key")
	}
	return key, nil
}
func (c *Client) external(o Object) (Object, error) {
	if !strings.HasPrefix(o.Key, c.prefix) || !validKey(o.Key) {
		return Object{}, errors.New("storage: provider returned invalid key")
	}
	o.Key = strings.TrimPrefix(o.Key, c.prefix)
	return o, nil
}
func (c *Client) Put(ctx context.Context, key string, body io.Reader, size int64, opts PutOptions) (Object, error) {
	key, err := c.key(ctx, key)
	if err != nil {
		return Object{}, err
	}
	if body == nil || size < 0 {
		return Object{}, errors.New("storage: reader and nonnegative size required")
	}
	r := &sourceReader{ctx: ctx, body: body, total: size, progress: opts.OnProgress}
	opts.OnProgress = nil // progress describes source reads, never SDK retries
	opts.Metadata = copyMap(opts.Metadata)
	o, err := c.backend.Put(ctx, key, r, size, opts)
	if err != nil {
		return Object{}, err
	}
	r.mu.Lock()
	read, eof, sourceErr := r.read, r.eof, r.failed
	r.mu.Unlock()
	if sourceErr != nil {
		return Object{}, sourceErr
	}
	if read != size {
		return Object{}, ErrSizeMismatch
	}
	if !eof {
		var extra [1]byte
		n, err := r.Read(extra[:])
		if n != 0 || err != io.EOF {
			if err != nil {
				return Object{}, err
			}
			return Object{}, ErrSizeMismatch
		}
	}
	o.Key = key
	return c.external(o)
}
func (c *Client) PutFile(ctx context.Context, key, path string, opts PutOptions) (Object, error) {
	f, err := os.Open(path)
	if err != nil {
		return Object{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Object{}, err
	}
	if !info.Mode().IsRegular() {
		return Object{}, errors.New("storage: source is not a regular file")
	}
	return c.Put(ctx, key, f, info.Size(), opts)
}
func (c *Client) Get(ctx context.Context, key string, opts GetOptions) (*Reader, error) {
	key, err := c.key(ctx, key)
	if err != nil {
		return nil, err
	}
	if opts.Offset < 0 || opts.Length < 0 || opts.Length > math.MaxInt64-opts.Offset {
		return nil, errors.New("storage: invalid range")
	}
	r, err := c.backend.Get(ctx, key, opts)
	if err != nil {
		return nil, err
	}
	if r == nil || r.ReadCloser == nil {
		return nil, errors.New("storage: provider returned no body")
	}
	r.Object.Key = key
	r.Object, err = c.external(r.Object)
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}
func (c *Client) ReadAll(ctx context.Context, key string) ([]byte, error) {
	r, err := c.Get(ctx, key, GetOptions{})
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if r.Length > c.maxReadBytes {
		return nil, errors.New("storage: read limit exceeded")
	}
	data, err := io.ReadAll(io.LimitReader(r, c.maxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > c.maxReadBytes {
		return nil, errors.New("storage: read limit exceeded")
	}
	return data, nil
}
func (c *Client) Stat(ctx context.Context, key string) (Object, error) {
	key, err := c.key(ctx, key)
	if err != nil {
		return Object{}, err
	}
	o, err := c.backend.Stat(ctx, key)
	if err != nil {
		return Object{}, err
	}
	o.Key = key
	return c.external(o)
}
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
func (c *Client) Delete(ctx context.Context, key string) error {
	key, err := c.key(ctx, key)
	if err != nil {
		return err
	}
	return c.backend.Delete(ctx, key)
}
func (c *Client) List(ctx context.Context, opts ListOptions) (Page, error) {
	if _, err := c.key(ctx, "validation"); err != nil {
		return Page{}, err
	}
	if opts.Prefix != "" && !validKey(opts.Prefix) {
		return Page{}, errors.New("storage: invalid list prefix")
	}
	if opts.Limit < 0 || opts.Limit > 1000 {
		return Page{}, errors.New("storage: page limit must be 1..1000")
	}
	if opts.Limit == 0 {
		opts.Limit = 100
	}
	opts.Prefix = c.prefix + opts.Prefix
	page, err := c.backend.List(ctx, opts)
	if err != nil {
		return Page{}, err
	}
	objects := make([]Object, 0, len(page.Objects))
	for _, o := range page.Objects {
		if c.prefix != "" && o.Key == c.prefix {
			continue
		} // hide namespace root marker
		if !strings.HasPrefix(o.Key, opts.Prefix) {
			return Page{}, errors.New("storage: provider returned a key outside list prefix")
		}
		o, err = c.external(o)
		if err != nil {
			return Page{}, err
		}
		objects = append(objects, o)
	}
	page.Objects = objects
	return page, nil
}
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (SignedURL, error) {
	key, err := c.key(ctx, key)
	if err != nil {
		return SignedURL{}, err
	}
	if ttl < time.Second {
		return SignedURL{}, errors.New("storage: URL lifetime must be at least one second")
	}
	return c.backend.PresignGet(ctx, key, ttl)
}
func copyMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

type sourceReader struct {
	mu          sync.Mutex
	ctx         context.Context
	body        io.Reader
	read, total int64
	progress    func(int64, int64) error
	eof         bool
	failed      error
}

func (r *sourceReader) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if err != nil && err != io.EOF {
			r.failed = err
		}
	}()
	if r.failed != nil {
		return 0, r.failed
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.eof {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.read == r.total {
		var extra [1]byte
		n, err := r.body.Read(extra[:])
		if n > 0 {
			return 0, ErrSizeMismatch
		}
		if err == io.EOF {
			r.eof = true
		}
		return 0, err
	}
	if int64(len(p)) > r.total-r.read {
		p = p[:r.total-r.read]
	}
	n, err = r.body.Read(p)
	r.read += int64(n)
	if n > 0 && r.progress != nil {
		if e := r.progress(r.read, r.total); e != nil {
			return n, e
		}
	}
	if err == io.EOF {
		if r.read != r.total {
			return n, ErrSizeMismatch
		}
		r.eof = true
	}
	return n, err
}

// Error normalizes provider errors without leaking request URLs or credentials.
type Error struct {
	Provider, Operation, Code, RequestID string
	StatusCode                           int
	Cause                                error
	Kind                                 error
}

func (e *Error) Error() string {
	return fmt.Sprintf("storage: %s %s failed (HTTP %d, code %s)", e.Provider, e.Operation, e.StatusCode, e.Code)
}
func (e *Error) Unwrap() error        { return e.Cause }
func (e *Error) Is(target error) bool { return e.Kind != nil && target == e.Kind }
func (e *Error) ErrorInfo() (string, string) {
	return "storage.operation_failed", "Object storage operation failed"
}
func (e *Error) ErrorFields() map[string]string {
	return map[string]string{"provider": e.Provider, "operation": e.Operation, "upstream_code": e.Code, "request_id": e.RequestID}
}
