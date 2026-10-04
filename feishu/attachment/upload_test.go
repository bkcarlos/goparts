package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc, configure func(*Config)) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "test-token", nil }}
	if configure != nil {
		configure(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func multipartValues(t *testing.T, r *http.Request) (map[string]string, string) {
	t.Helper()
	if r.ContentLength <= 0 || len(r.TransferEncoding) > 0 {
		t.Error("multipart length must be known")
	}
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Error("incorrect token")
	}
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Error(err)
		return nil, ""
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	fields := map[string]string{}
	filename := ""
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Error(err)
			break
		}
		value, err := io.ReadAll(part)
		if err != nil {
			t.Error(err)
		}
		fields[part.FormName()] = string(value)
		if part.FormName() == "file" {
			filename = part.FileName()
		}
	}
	return fields, filename
}

func TestUploadAndSendProtocols(t *testing.T) {
	var calls []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path == "/im/v1/messages" {
			var body struct{ ReceiveID, MsgType, Content, UUID string }
			var raw map[string]string
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Error(err)
			}
			body.ReceiveID, body.MsgType, body.Content, body.UUID = raw["receive_id"], raw["msg_type"], raw["content"], raw["uuid"]
			if r.URL.Query().Get("receive_id_type") != "chat_id" || body.ReceiveID != "oc1" || body.MsgType != "file" || body.Content != `{"file_key":"file_key1"}` || body.UUID != "send-1" {
				t.Errorf("invalid file message: %+v", raw)
			}
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om1","chat_id":"oc1"}}`)
			return
		}
		fields, filename := multipartValues(t, r)
		if fields["file"] != "hello" || filename != "报告.txt" || fields["file_name"] != "报告.txt" {
			t.Errorf("multipart=%v filename=%q", fields, filename)
		}
		switch r.URL.Path {
		case "/im/v1/files":
			if fields["file_type"] != "stream" || fields["duration"] != "100" {
				t.Errorf("chat fields=%v", fields)
			}
			fmt.Fprint(w, `{"code":0,"data":{"file_key":"file_key1"}}`)
		case "/drive/v1/files/upload_all":
			if fields["size"] != "5" || fields["parent_type"] != "explorer" || fields["parent_node"] != "folder1" {
				t.Errorf("drive fields=%v", fields)
			}
			fmt.Fprint(w, `{"code":0,"data":{"file_token":"drive1"}}`)
		case "/drive/v1/medias/upload_all":
			if fields["size"] != "5" || fields["parent_type"] != "docx_file" || fields["parent_node"] != "block1" || fields["extra"] != `{"drive_route_token":"doc1"}` {
				t.Errorf("media fields=%v", fields)
			}
			fmt.Fprint(w, `{"code":0,"data":{"file_token":"media1"}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}, nil)
	ctx := context.Background()
	source := func() Source { return Source{strings.NewReader("hello"), 5} }
	var progress atomic.Int64
	opts := UploadOptions{FileName: "报告.txt", DurationMS: 100, OnProgress: func(read, total int64) error {
		if read > total || total != 5 {
			t.Error("invalid progress")
		}
		progress.Store(read)
		return nil
	}}
	file, err := c.UploadChat(ctx, source(), opts)
	if err != nil || file.FileKey != "file_key1" || progress.Load() != 5 {
		t.Fatalf("chat=%+v %v", file, err)
	}
	message, err := c.SendFile(ctx, Chat("oc1"), file.FileKey, SendOptions{UUID: "send-1"})
	if err != nil || message.MessageID != "om1" {
		t.Fatalf("message=%+v %v", message, err)
	}
	drive, err := c.UploadDrive(ctx, "folder1", source(), opts)
	if err != nil || drive.FileToken != "drive1" {
		t.Fatalf("drive=%+v %v", drive, err)
	}
	media, err := c.UploadMedia(ctx, MediaTarget{"docx_file", "block1", "doc1"}, source(), opts)
	if err != nil || media.FileToken != "media1" {
		t.Fatalf("media=%+v %v", media, err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestPathHelpers(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fields, name := multipartValues(t, r)
		if name != "note.txt" || fields["file"] != "hello" {
			t.Error("path did not supply name/content")
		}
		fmt.Fprint(w, `{"code":0,"data":{"file_key":"file1","file_token":"token1"}}`)
	}, nil)
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.UploadChatPath(ctx, path, UploadOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UploadDrivePath(ctx, "folder1", path, UploadOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UploadMediaPath(ctx, MediaTarget{"docx_file", "block1", "doc1"}, path, UploadOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UploadChatPath(ctx, filepath.Dir(path), UploadOptions{}); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := c.UploadChatPath(ctx, path+"missing", UploadOptions{}); err == nil {
		t.Fatal("missing path accepted")
	}
}

func TestLimitsAndValidationBeforeAuth(t *testing.T) {
	var auth atomic.Int32
	c, err := New(Config{MaxFileBytes: 4, TokenProvider: func(context.Context) (string, error) { auth.Add(1); return "token", nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	opts := UploadOptions{FileName: "file.txt"}
	for _, source := range []Source{{strings.NewReader("hello"), 5}, {strings.NewReader(""), 0}, {nil, 1}} {
		if _, err := c.UploadChat(ctx, source, opts); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	for _, name := range []string{"", "../file", "a\r\nx: y", "dir/file", strings.Repeat("a", 251)} {
		if _, err := c.UploadChat(ctx, Source{strings.NewReader("a"), 1}, UploadOptions{FileName: name}); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if _, err := c.UploadChat(ctx, Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a", FileType: "unknown"}); err == nil {
		t.Fatal("invalid type accepted")
	}
	if _, err := c.UploadDrive(ctx, "", Source{}, opts); err == nil {
		t.Fatal("missing folder accepted")
	}
	if _, err := c.UploadMedia(ctx, MediaTarget{"docx_file", "block", ""}, Source{}, opts); err == nil {
		t.Fatal("missing route accepted")
	}
	if _, err := c.SendFile(ctx, User(""), "file1", SendOptions{}); err == nil {
		t.Fatal("empty receiver accepted")
	}
	if auth.Load() != 0 {
		t.Fatal("invalid input reached token provider")
	}
	c.maxFileBytes = 100 * 1024 * 1024
	if _, err := c.UploadChat(ctx, Source{strings.NewReader("x"), MaxChatFileBytes + 1}, opts); !errors.Is(err, ErrFileTooLarge) {
		t.Fatal("platform chat limit bypassed")
	}
	if _, err := c.UploadDrive(ctx, "folder1", Source{strings.NewReader("x"), MaxDriveFileBytes + 1}, opts); !errors.Is(err, ErrFileTooLarge) {
		t.Fatal("platform drive limit bypassed")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReaderFailuresAndProgressCancellation(t *testing.T) {
	errStop := errors.New("stop-progress")
	for _, tc := range []struct {
		name, text string
		size       int64
		progress   func(int64, int64) error
		want       error
	}{
		{"short", "ab", 3, nil, ErrSizeMismatch}, {"long", "abcd", 3, nil, ErrSizeMismatch}, {"callback", "abc", 3, func(int64, int64) error { return errStop }, errStop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(Config{TokenProvider: func(context.Context) (string, error) { return "token", nil }, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				_, err := io.Copy(io.Discard, r.Body)
				return nil, err
			})}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.UploadChat(context.Background(), Source{strings.NewReader(tc.text), tc.size}, UploadOptions{FileName: "file.txt", OnProgress: tc.progress})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestAPIFailuresNoRetriesAndRedirect(t *testing.T) {
	for _, tc := range []struct {
		body    string
		status  int
		wantAPI bool
	}{
		{`{"code":234006,"msg":"private-body"}`, 400, true}, {`{"code":1061001}`, 200, true}, {`bad gateway`, 502, true}, {`{"data":{}}`, 200, false}, {`{"code":0,"data":{}}`, 200, false},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			var calls atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.Copy(io.Discard, r.Body)
				w.Header().Set("X-Tt-Logid", "req1")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}, nil)
			_, err := c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"})
			if err == nil || calls.Load() != 1 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
			var api *APIError
			if tc.wantAPI {
				if !errors.As(err, &api) || api.RequestID != "req1" || strings.Contains(err.Error(), "private-body") {
					t.Fatalf("api error=%v", err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	var forwarded atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer sink.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, sink.URL, 307) }, nil)
	if _, err := c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"}); err == nil || forwarded.Load() != 0 {
		t.Fatal("upload redirected")
	}
}

func TestCancellationResponseLimitAndMediaSerialization(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, strings.Repeat("x", 20))
	}, func(cfg *Config) { cfg.MaxResponseBytes = 10 })
	if _, err := c.UploadChat(context.Background(), Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"}); err == nil {
		t.Fatal("response limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.UploadChat(ctx, Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	var active atomic.Int32
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if active.Add(1) != 1 {
			t.Error("media uploads ran concurrently")
		}
		defer active.Add(-1)
		io.Copy(io.Discard, r.Body)
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, `{"code":0,"data":{"file_token":"media1"}}`)
	}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.UploadMedia(context.Background(), MediaTarget{"docx_file", "block1", "doc1"}, Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	c.mediaGate <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.UploadMedia(ctx, MediaTarget{"docx_file", "block1", "doc1"}, Source{strings.NewReader("a"), 1}, UploadOptions{FileName: "a"})
	<-c.mediaGate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("gate cancellation=%v", err)
	}
}
