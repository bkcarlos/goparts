package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

type UploadOptions struct {
	FileName   string                        // optional for path helpers; required for Reader uploads
	FileType   string                        // chat files only; default stream, no type sniffing or transcoding
	DurationMS int64                         // optional chat audio/video duration
	OnProgress func(read, total int64) error // synchronous source-read progress; errors abort upload
}

// Source is consumed once. Reader belongs to the caller and must eventually
// return; context cancellation cannot interrupt an arbitrary blocked Reader.
type Source struct {
	Reader io.Reader
	Size   int64
}
type ChatFile struct {
	FileKey string `json:"file_key"`
}
type StoredFile struct {
	FileToken string `json:"file_token"`
}

type MediaTarget struct {
	ParentType    string // docx_file/docx_image/sheet_file/sheet_image/bitable_file/bitable_image
	ParentNode    string // docx uses a FILE/IMAGE BLOCK ID, not the document ID
	DocumentToken string // serialized as extra.drive_route_token
}

func (c *Client) UploadChat(ctx context.Context, source Source, opts UploadOptions) (ChatFile, error) {
	if opts.FileType == "" {
		opts.FileType = "stream"
	}
	switch opts.FileType {
	case "stream", "pdf", "doc", "xls", "ppt", "opus", "mp4":
	default:
		return ChatFile{}, errors.New("feishu/attachment: unsupported chat file type")
	}
	if opts.DurationMS < 0 {
		return ChatFile{}, errors.New("feishu/attachment: duration must not be negative")
	}
	fields := [][2]string{{"file_type", opts.FileType}, {"file_name", opts.FileName}}
	if opts.DurationMS > 0 {
		fields = append(fields, [2]string{"duration", strconv.FormatInt(opts.DurationMS, 10)})
	}
	var result ChatFile
	err := c.upload(ctx, "/im/v1/files", source, opts, MaxChatFileBytes, fields, &result, false)
	if err == nil && result.FileKey == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

func (c *Client) UploadDrive(ctx context.Context, folderToken string, source Source, opts UploadOptions) (StoredFile, error) {
	if !validToken(folderToken) {
		return StoredFile{}, errors.New("feishu/attachment: folder token is required")
	}
	fields := [][2]string{{"file_name", opts.FileName}, {"parent_type", "explorer"}, {"parent_node", folderToken}, {"size", strconv.FormatInt(source.Size, 10)}}
	var result StoredFile
	err := c.upload(ctx, "/drive/v1/files/upload_all", source, opts, MaxDriveFileBytes, fields, &result, false)
	if err == nil && result.FileToken == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

func (c *Client) UploadMedia(ctx context.Context, target MediaTarget, source Source, opts UploadOptions) (StoredFile, error) {
	switch target.ParentType {
	case "docx_file", "docx_image", "sheet_file", "sheet_image", "bitable_file", "bitable_image":
	default:
		return StoredFile{}, errors.New("feishu/attachment: unsupported media parent type")
	}
	if !validToken(target.ParentNode) || !validToken(target.DocumentToken) {
		return StoredFile{}, errors.New("feishu/attachment: parent node and document token are required")
	}
	extra, _ := json.Marshal(map[string]string{"drive_route_token": target.DocumentToken})
	fields := [][2]string{{"file_name", opts.FileName}, {"parent_type", target.ParentType}, {"parent_node", target.ParentNode}, {"size", strconv.FormatInt(source.Size, 10)}, {"extra", string(extra)}}
	var result StoredFile
	err := c.upload(ctx, "/drive/v1/medias/upload_all", source, opts, MaxDriveFileBytes, fields, &result, true)
	if err == nil && result.FileToken == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

func (c *Client) UploadChatPath(ctx context.Context, path string, opts UploadOptions) (ChatFile, error) {
	f, source, opts, err := openSource(path, opts)
	if err != nil {
		return ChatFile{}, err
	}
	defer f.Close()
	return c.UploadChat(ctx, source, opts)
}
func (c *Client) UploadDrivePath(ctx context.Context, folderToken, path string, opts UploadOptions) (StoredFile, error) {
	f, source, opts, err := openSource(path, opts)
	if err != nil {
		return StoredFile{}, err
	}
	defer f.Close()
	return c.UploadDrive(ctx, folderToken, source, opts)
}
func (c *Client) UploadMediaPath(ctx context.Context, target MediaTarget, path string, opts UploadOptions) (StoredFile, error) {
	f, source, opts, err := openSource(path, opts)
	if err != nil {
		return StoredFile{}, err
	}
	defer f.Close()
	return c.UploadMedia(ctx, target, source, opts)
}

func openSource(path string, opts UploadOptions) (*os.File, Source, UploadOptions, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Source{}, opts, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, Source{}, opts, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, Source{}, opts, errors.New("feishu/attachment: path must point to a regular file")
	}
	if opts.FileName == "" {
		opts.FileName = filepath.Base(path)
	}
	return f, Source{Reader: f, Size: info.Size()}, opts, nil
}

func (c *Client) upload(ctx context.Context, path string, source Source, opts UploadOptions, platformLimit int64, fields [][2]string, output any, serialize bool) error {
	if source.Reader == nil || source.Size <= 0 {
		return errors.New("feishu/attachment: a nonempty reader and positive size are required")
	}
	if source.Size > platformLimit || source.Size > c.maxFileBytes {
		return ErrFileTooLarge
	}
	if !utf8.ValidString(opts.FileName) || strings.TrimSpace(opts.FileName) == "" || utf8.RuneCountInString(opts.FileName) > 250 || strings.ContainsAny(opts.FileName, "\r\n\x00/\\") {
		return errors.New("feishu/attachment: invalid file name")
	}
	ctx, cancel, err := c.context(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if serialize {
		select {
		case c.mediaGate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-c.mediaGate }()
	}
	// Only multipart headers/boundaries are buffered, never the file body.
	var buffer bytes.Buffer
	w := multipart.NewWriter(&buffer)
	for _, field := range fields {
		if err = w.WriteField(field[0], field[1]); err != nil {
			return err
		}
	}
	if _, err = w.CreateFormFile("file", opts.FileName); err != nil {
		return err
	}
	prefix := bytes.Clone(buffer.Bytes())
	if err = w.Close(); err != nil {
		return err
	}
	suffix := bytes.Clone(buffer.Bytes()[len(prefix):])
	reader := &fileReader{ctx: ctx, source: source.Reader, total: source.Size, progress: opts.OnProgress}
	body := io.MultiReader(bytes.NewReader(prefix), reader, bytes.NewReader(suffix))
	err = c.request(ctx, path, w.FormDataContentType(), body, int64(len(prefix)+len(suffix))+source.Size, output)
	if err == nil && !reader.finished.Load() {
		return errors.New("feishu/attachment: server returned success before file upload completed")
	}
	return err
}

func validToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

type fileReader struct {
	ctx         context.Context
	source      io.Reader
	total, read int64
	progress    func(int64, int64) error
	finished    atomic.Bool
}

func (r *fileReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.finished.Load() {
		return 0, io.EOF
	}
	if r.read == r.total {
		// Probe once past the declaration before allowing the closing boundary.
		var extra [1]byte
		n, err := r.source.Read(extra[:])
		if n > 0 {
			return 0, ErrSizeMismatch
		}
		if err == io.EOF {
			r.finished.Store(true)
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		return 0, nil
	}
	if int64(len(p)) > r.total-r.read {
		p = p[:int(r.total-r.read)]
	}
	n, err := r.source.Read(p)
	r.read += int64(n)
	if n > 0 && r.progress != nil {
		if progressErr := r.progress(r.read, r.total); progressErr != nil {
			return n, fmt.Errorf("feishu/attachment: progress callback: %w", progressErr)
		}
	}
	if err == io.EOF {
		if r.read != r.total {
			return n, ErrSizeMismatch
		}
		r.finished.Store(true)
	}
	return n, err
}
