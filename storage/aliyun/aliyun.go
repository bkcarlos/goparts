// Package aliyun adapts Alibaba Cloud OSS to the generic storage.Backend.
package aliyun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/bkcarlos/goparts/storage"
)

type Credentials struct {
	AccessKeyID, AccessKeySecret, SecurityToken string
	ExpiresAt                                   time.Time
}

func (Credentials) String() string   { return "aliyun credentials (redacted)" }
func (Credentials) GoString() string { return "aliyun credentials (redacted)" }

type Config struct {
	Bucket, Region, Endpoint string
	Credentials              Credentials
	CredentialsProvider      func(context.Context) (Credentials, error) // optional rotating STS credentials
	HTTPClient               *http.Client
	RequestTimeout           time.Duration // default 2 minutes, includes streamed response reads
	CleanupTimeout           time.Duration // default 10s, independent of canceled upload context
	MultipartThreshold       int64         // default 100 MiB
	PartSize                 int64         // default 8 MiB; 100 KiB..1 GiB
	Parallelism              int           // default 3; 1..64
	UsePathStyle             bool          // useful for compatible gateways/local tests
	MaxResponseBytes         int64         // metadata/error responses only; default 4 MiB
}
type Backend struct {
	client *oss.Client
	cfg    Config
}

var _ storage.Backend = (*Backend)(nil)

func New(cfg Config) (*Backend, error) {
	if cfg.Bucket == "" || strings.ContainsAny(cfg.Bucket, "/\\\r\n ") || cfg.Region == "" {
		return nil, errors.New("storage/aliyun: bucket and region required")
	}
	if cfg.CredentialsProvider == nil && (cfg.Credentials.AccessKeyID == "" || cfg.Credentials.AccessKeySecret == "") {
		return nil, errors.New("storage/aliyun: credentials required")
	}
	if cfg.RequestTimeout < 0 || cfg.CleanupTimeout < 0 || cfg.MultipartThreshold < 0 || cfg.PartSize < 0 || cfg.Parallelism < 0 || cfg.Parallelism > 64 || cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes == math.MaxInt64 {
		return nil, errors.New("storage/aliyun: invalid timeout or upload configuration")
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 2 * time.Minute
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 4 * 1024 * 1024
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = 10 * time.Second
	}
	if cfg.MultipartThreshold == 0 {
		cfg.MultipartThreshold = 100 * 1024 * 1024
	}
	if cfg.PartSize == 0 {
		cfg.PartSize = 8 * 1024 * 1024
	}
	if cfg.Parallelism == 0 {
		cfg.Parallelism = 3
	}
	if cfg.PartSize < 100*1024 || cfg.PartSize > 1024*1024*1024 {
		return nil, errors.New("storage/aliyun: PartSize must be 100 KiB..1 GiB")
	}
	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
			return nil, errors.New("storage/aliyun: endpoint must be HTTP(S) origin")
		}
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		*hc = *cfg.HTTPClient
	}
	if hc.Timeout == 0 || hc.Timeout > cfg.RequestTimeout {
		hc.Timeout = cfg.RequestTimeout
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	hc.Transport = &metadataTransport{next: transport, limit: cfg.MaxResponseBytes}
	provider := credentials.CredentialsProviderFunc(func(ctx context.Context) (credentials.Credentials, error) {
		v := cfg.Credentials
		if cfg.CredentialsProvider != nil {
			var err error
			v, err = cfg.CredentialsProvider(ctx)
			if err != nil {
				return credentials.Credentials{}, err
			}
		}
		if v.AccessKeyID == "" || v.AccessKeySecret == "" || !v.ExpiresAt.IsZero() && !v.ExpiresAt.After(time.Now()) {
			return credentials.Credentials{}, errors.New("storage/aliyun: missing or expired credentials")
		}
		out := credentials.Credentials{AccessKeyID: v.AccessKeyID, AccessKeySecret: v.AccessKeySecret, SecurityToken: v.SecurityToken}
		if !v.ExpiresAt.IsZero() {
			out.Expires = &v.ExpiresAt
		}
		return out, nil
	})
	opts := oss.LoadDefaultConfig().WithRegion(cfg.Region).WithCredentialsProvider(provider).WithHttpClient(hc).WithRetryMaxAttempts(1).WithEnabledRedirect(false).WithUsePathStyle(cfg.UsePathStyle)
	if cfg.Endpoint != "" {
		opts.WithEndpoint(cfg.Endpoint)
	}
	return &Backend{client: oss.NewClient(opts), cfg: cfg}, nil
}
func (b *Backend) Put(ctx context.Context, key string, body io.Reader, size int64, opts storage.PutOptions) (storage.Object, error) {
	if ctx == nil || body == nil || size < 0 {
		return storage.Object{}, errors.New("storage/aliyun: context, reader and size required")
	}
	req := &oss.PutObjectRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key), Body: body, ContentLength: oss.Ptr(size), Metadata: opts.Metadata}
	if opts.ContentType != "" {
		req.ContentType = oss.Ptr(opts.ContentType)
	}
	object := storage.Object{Key: key, Size: size, ContentType: opts.ContentType, Metadata: opts.Metadata}
	if size < b.cfg.MultipartThreshold {
		result, err := b.client.PutObject(ctx, req)
		if err != nil {
			return storage.Object{}, wrap("put", err)
		}
		object.ETag = oss.ToString(result.ETag)
		object.VersionID = oss.ToString(result.VersionId)
		return object, nil
	}
	partSize := b.cfg.PartSize
	if required := (size-1)/10000 + 1; required > partSize {
		partSize = required
	}
	if partSize > 1024*1024*1024 {
		return storage.Object{}, errors.New("storage/aliyun: object exceeds multipart capacity")
	}
	uploader := oss.NewUploader(b.client, func(o *oss.UploaderOptions) {
		o.PartSize = partSize
		o.ParallelNum = b.cfg.Parallelism
		o.LeavePartsOnError = true
	})
	result, err := uploader.UploadFrom(ctx, req, body)
	if err != nil {
		mapped := wrap("put", err)
		var uploadErr *oss.UploadError
		if errors.As(err, &uploadErr) && uploadErr.UploadId != "" {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.cfg.CleanupTimeout)
			defer cancel()
			_, cleanupErr := b.client.AbortMultipartUpload(cleanup, &oss.AbortMultipartUploadRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key), UploadId: oss.Ptr(uploadErr.UploadId)})
			if cleanupErr != nil {
				return storage.Object{}, errors.Join(mapped, wrap("abort_multipart", cleanupErr))
			}
		}
		return storage.Object{}, mapped
	}
	object.ETag = oss.ToString(result.ETag)
	object.VersionID = oss.ToString(result.VersionId)
	return object, nil
}
func (b *Backend) Get(ctx context.Context, key string, opts storage.GetOptions) (*storage.Reader, error) {
	req := &oss.GetObjectRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key)}
	if opts.IfMatch != "" {
		req.IfMatch = oss.Ptr(opts.IfMatch)
	}
	ranged := opts.Offset > 0 || opts.Length > 0
	if ranged {
		end := ""
		if opts.Length > 0 {
			end = strconv.FormatInt(opts.Offset+opts.Length-1, 10)
		}
		req.Range = oss.Ptr(fmt.Sprintf("bytes=%d-%s", opts.Offset, end))
		req.RangeBehavior = oss.Ptr("standard")
	}
	result, err := b.client.GetObject(ctx, req)
	if err != nil {
		return nil, wrap("get", err)
	}
	size := result.ContentLength
	if ranged {
		var start, end, total int64
		if result.StatusCode != 206 {
			result.Body.Close()
			return nil, storage.ErrUnsupported
		}
		if n, err := fmt.Sscanf(oss.ToString(result.ContentRange), "bytes %d-%d/%d", &start, &end, &total); n != 3 || err != nil || start != opts.Offset || end < start || total <= end || end-start+1 != result.ContentLength || (opts.Length > 0 && result.ContentLength > opts.Length) {
			result.Body.Close()
			return nil, errors.New("storage/aliyun: invalid range response")
		}
		size = total
	}
	return &storage.Reader{ReadCloser: result.Body, Object: storage.Object{Key: key, Size: size, ETag: oss.ToString(result.ETag), ContentType: oss.ToString(result.ContentType), LastModified: oss.ToTime(result.LastModified), Metadata: result.Metadata, VersionID: oss.ToString(result.VersionId)}, Offset: opts.Offset, Length: result.ContentLength}, nil
}
func (b *Backend) Stat(ctx context.Context, key string) (storage.Object, error) {
	r, err := b.client.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key)})
	if err != nil {
		return storage.Object{}, wrap("stat", err)
	}
	return storage.Object{Key: key, Size: r.ContentLength, ETag: oss.ToString(r.ETag), ContentType: oss.ToString(r.ContentType), LastModified: oss.ToTime(r.LastModified), Metadata: r.Metadata, VersionID: oss.ToString(r.VersionId)}, nil
}
func (b *Backend) Delete(ctx context.Context, key string) error {
	_, err := b.client.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key)})
	return wrap("delete", err)
}
func (b *Backend) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	r, err := b.client.ListObjectsV2(ctx, &oss.ListObjectsV2Request{Bucket: oss.Ptr(b.cfg.Bucket), Prefix: oss.Ptr(opts.Prefix), ContinuationToken: oss.Ptr(opts.Cursor), MaxKeys: int32(opts.Limit)})
	if err != nil {
		return storage.Page{}, wrap("list", err)
	}
	page := storage.Page{}
	for _, o := range r.Contents {
		page.Objects = append(page.Objects, storage.Object{Key: oss.ToString(o.Key), Size: o.Size, ETag: oss.ToString(o.ETag), LastModified: oss.ToTime(o.LastModified)})
	}
	if r.IsTruncated {
		page.NextCursor = oss.ToString(r.NextContinuationToken)
		if page.NextCursor == "" {
			return storage.Page{}, errors.New("storage/aliyun: truncated listing without cursor")
		}
	}
	return page, nil
}
func (b *Backend) PresignGet(ctx context.Context, key string, ttl time.Duration) (storage.SignedURL, error) {
	if ttl < time.Second || ttl > 7*24*time.Hour {
		return storage.SignedURL{}, errors.New("storage/aliyun: signed URL lifetime must be 1s..7d")
	}
	r, err := b.client.Presign(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(b.cfg.Bucket), Key: oss.Ptr(key)}, func(o *oss.PresignOptions) { o.Expires = ttl })
	if err != nil {
		return storage.SignedURL{}, wrap("presign", err)
	}
	return storage.SignedURL{URL: r.URL, ExpiresAt: r.Expiration}, nil
}
func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	e := &storage.Error{Provider: "aliyun", Operation: operation, Cause: err}
	var service *oss.ServiceError
	if errors.As(err, &service) {
		e.Code = service.Code
		e.StatusCode = service.StatusCode
		e.RequestID = service.RequestID
		switch {
		case service.Code == "NoSuchKey":
			e.Kind = storage.ErrNotFound
		case service.StatusCode == 401 || service.StatusCode == 403:
			e.Kind = storage.ErrPermission
		case service.StatusCode == 412:
			e.Kind = storage.ErrPrecondition
		}
	}
	return e
}
