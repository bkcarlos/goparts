package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type RangeInfo struct {
	Size      int64
	ETag      string
	Identity  string
	Supported bool
}

// RangeSource keeps parallel/resume behavior independent of object-store vendors.
// OpenRange must return exactly length bytes of the version identified by ETag.
type RangeSource interface {
	Source
	Probe(context.Context) (RangeInfo, error)
	OpenRange(context.Context, int64, int64, string) (Stream, error)
}
type RangeMode string

const (
	Auto     RangeMode = "auto"
	Single   RangeMode = "single"
	Parallel RangeMode = "parallel"
)

type RangeOptions struct {
	Options
	Mode       RangeMode
	Workers    int
	ChunkBytes int64
	Resume     bool
}

func (s *HTTPSource) Probe(ctx context.Context) (RangeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint, nil)
	if err != nil {
		return RangeInfo{}, err
	}
	req.Header = s.headers.Clone()
	req.Header.Set("Range", "bytes=0-0")
	resp, err := s.client.Do(req)
	if err != nil {
		return RangeInfo{}, &HTTPError{Cause: err}
	}
	defer resp.Body.Close()
	identityData, _ := json.Marshal(struct {
		URL     string
		Headers http.Header
	}{s.endpoint, s.headers})
	identity := sha256.Sum256(identityData)
	info := RangeInfo{Identity: hex.EncodeToString(identity[:]), Size: resp.ContentLength, ETag: resp.Header.Get("ETag")}
	if resp.StatusCode == http.StatusOK {
		return info, nil
	}
	if resp.StatusCode == 416 && resp.Header.Get("Content-Range") == "bytes */0" {
		info.Size = 0
		return info, nil
	}
	if resp.StatusCode != 206 {
		return info, &HTTPError{StatusCode: resp.StatusCode}
	}
	start, end, total, err := parseRange(resp.Header.Get("Content-Range"))
	if err != nil || start != 0 || end != 0 || total < 1 {
		return info, ErrSizeMismatch
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2))
	if err != nil || len(b) != 1 {
		return info, ErrSizeMismatch
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return info, errors.New("download: unexpected encoding")
	}
	info.Size = total
	info.Supported = true
	return info, nil
}
func (s *HTTPSource) RangeSupport(ctx context.Context) (bool, error) {
	info, err := s.Probe(ctx)
	return info.Supported, err
}
func parseRange(text string) (start, end, total int64, err error) {
	if !strings.HasPrefix(text, "bytes ") {
		return 0, 0, 0, ErrSizeMismatch
	}
	span, size, ok := strings.Cut(strings.TrimPrefix(text, "bytes "), "/")
	if !ok {
		return 0, 0, 0, ErrSizeMismatch
	}
	a, b, ok := strings.Cut(span, "-")
	if !ok {
		return 0, 0, 0, ErrSizeMismatch
	}
	start, err = strconv.ParseInt(a, 10, 64)
	if err != nil {
		return
	}
	end, err = strconv.ParseInt(b, 10, 64)
	if err != nil {
		return
	}
	total, err = strconv.ParseInt(size, 10, 64)
	if err == nil && (start < 0 || end < start || total <= end) {
		err = ErrSizeMismatch
	}
	return
}
func (s *HTTPSource) OpenRange(ctx context.Context, offset, length int64, etag string) (Stream, error) {
	if offset < 0 || length < 1 || offset > int64(^uint64(0)>>1)-length {
		return Stream{}, errors.New("download: invalid range")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint, nil)
	if err != nil {
		return Stream{}, err
	}
	req.Header = s.headers.Clone()
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Stream{}, &HTTPError{Cause: err}
	}
	reject := func(err error) (Stream, error) { resp.Body.Close(); return Stream{}, err }
	if resp.StatusCode != 206 {
		return reject(&HTTPError{StatusCode: resp.StatusCode})
	}
	start, end, _, err := parseRange(resp.Header.Get("Content-Range"))
	if err != nil || start != offset || end != offset+length-1 || resp.ContentLength >= 0 && resp.ContentLength != length {
		return reject(ErrSizeMismatch)
	}
	if etag != "" && resp.Header.Get("ETag") != etag {
		return reject(errors.New("download: source version changed"))
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return reject(errors.New("download: unexpected encoding"))
	}
	return Stream{Body: resp.Body, Size: length}, nil
}

type rangeManifest struct {
	Identity, ETag, SHA256 string
	Size, ChunkBytes       int64
}

// FetchRanges downloads durable, independently completed chunks, then verifies
// and atomically publishes the assembled file. Resume keeps chunks on failure.
// It requires a strong ETag or expected SHA-256 to bind chunks to one version.
func (c *Client) FetchRanges(ctx context.Context, source RangeSource, destination string, opts RangeOptions) (Result, error) {
	if ctx == nil || source == nil || destination == "" || opts.Workers < 0 || opts.Workers > 64 || opts.ChunkBytes < 0 {
		return Result{}, errors.New("download: invalid range options")
	}
	if opts.Mode == Single {
		return c.Fetch(ctx, source, destination, opts.Options)
	}
	if opts.Mode != "" && opts.Mode != Auto && opts.Mode != Parallel {
		return Result{}, errors.New("download: invalid mode")
	}
	if opts.SHA256 != "" {
		b, err := hex.DecodeString(opts.SHA256)
		if err != nil || len(b) != 32 {
			return Result{}, errors.New("download: invalid SHA-256")
		}
	}
	if opts.Workers == 0 {
		opts.Workers = 4
	}
	if opts.ChunkBytes == 0 {
		opts.ChunkBytes = 8 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	info, err := source.Probe(ctx)
	if err != nil {
		return Result{}, err
	}
	if info.Size > c.cfg.MaxBytes {
		return Result{}, ErrTooLarge
	}
	strong := strings.HasPrefix(info.ETag, "\"") && strings.HasSuffix(info.ETag, "\"")
	if !info.Supported || info.Size <= 0 || !strong && opts.SHA256 == "" {
		if opts.Mode == Parallel {
			return Result{}, errors.New("download: stable range source required")
		}
		return c.Fetch(ctx, source, destination, opts.Options)
	}
	if !strong {
		info.ETag = ""
	}
	if info.Identity == "" {
		return Result{}, errors.New("download: source identity required")
	}
	if (info.Size-1)/opts.ChunkBytes+1 > 100000 {
		return Result{}, errors.New("download: too many chunks")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return Result{}, err
	}
	if _, err = os.Lstat(destination); err == nil && !opts.Overwrite {
		return Result{}, ErrExists
	}
	dir := destination + ".goparts-part"
	if !opts.Resume {
		dir, err = os.MkdirTemp(filepath.Dir(destination), ".goparts-ranges-*")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(dir)
	} else {
		if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return Result{}, err
		}
		st, e := os.Lstat(dir)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return Result{}, errors.New("download: invalid resume directory")
		}
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Result{}, errors.New("download: resume state is locked")
	}
	lock.Close()
	defer os.Remove(filepath.Join(dir, "lock"))
	manifest := rangeManifest{info.Identity, info.ETag, strings.ToLower(opts.SHA256), info.Size, opts.ChunkBytes}
	manifestPath := filepath.Join(dir, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err == nil {
		var prior rangeManifest
		if len(b) > 4096 || json.Unmarshal(b, &prior) != nil || prior != manifest {
			return Result{}, errors.New("download: resume source/version/options changed; remove old partial directory")
		}
	} else if os.IsNotExist(err) {
		b, _ = json.Marshal(manifest)
		if err = os.WriteFile(manifestPath, b, 0600); err != nil {
			return Result{}, err
		}
	} else {
		return Result{}, err
	}
	count := int((info.Size-1)/opts.ChunkBytes + 1)
	workCtx, stop := context.WithCancel(ctx)
	defer stop()
	jobs := make(chan int)
	failures := make(chan error, opts.Workers)
	var wg sync.WaitGroup
	for worker := 0; worker < opts.Workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				size := min(opts.ChunkBytes, info.Size-int64(i)*opts.ChunkBytes)
				path := filepath.Join(dir, fmt.Sprintf("chunk-%06d", i))
				if stat, err := os.Lstat(path); err == nil && stat.Mode().IsRegular() && stat.Size() == size && validChunk(path) {
					continue
				}
				stream, err := source.OpenRange(workCtx, int64(i)*opts.ChunkBytes, size, info.ETag)
				if err == nil {
					if stream.Body == nil {
						err = errors.New("download: missing range body")
					} else {
						err = errors.Join(writeChunk(workCtx, path, stream.Body, size), stream.Body.Close())
					}
				}
				if err != nil {
					failures <- err
					stop()
					return
				}
			}
		}()
	}
send:
	for i := 0; i < count; i++ {
		select {
		case jobs <- i:
		case <-workCtx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			return Result{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	joined := SourceFunc(func(context.Context) (Stream, error) {
		return Stream{Body: &chunkReader{dir: dir, count: count}, Size: info.Size}, nil
	})
	result, err := c.Fetch(ctx, joined, destination, opts.Options)
	if err == nil && opts.Resume {
		os.RemoveAll(dir)
	}
	return result, err
}
func writeChunk(ctx context.Context, path string, body io.Reader, size int64) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".chunk-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if n != size {
		return ErrSizeMismatch
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
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return os.WriteFile(path+".sha256", []byte(hex.EncodeToString(hash.Sum(nil))), 0600)
}

type chunkReader struct {
	dir          string
	count, index int
	file         *os.File
}

func (r *chunkReader) Read(p []byte) (int, error) {
	for {
		if r.file == nil {
			if r.index >= r.count {
				return 0, io.EOF
			}
			f, err := os.Open(filepath.Join(r.dir, fmt.Sprintf("chunk-%06d", r.index)))
			if err != nil {
				return 0, err
			}
			r.file = f
			r.index++
		}
		n, err := r.file.Read(p)
		if err == io.EOF {
			r.file.Close()
			r.file = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}
func (r *chunkReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

func validChunk(path string) bool {
	expected, err := os.ReadFile(path + ".sha256")
	if err != nil || len(expected) != 64 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return false
	}
	return string(expected) == hex.EncodeToString(h.Sum(nil))
}
