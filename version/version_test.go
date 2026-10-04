package version

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type memory struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memory) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.data[key] = b
	m.mu.Unlock()
	return nil
}
func (m *memory) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil
}
func TestPublishDownloadReplaceRollback(t *testing.T) {
	store := &memory{data: map[string][]byte{}}
	root := t.TempDir()
	binary := filepath.Join(root, "new")
	target := filepath.Join(root, "app")
	os.WriteFile(binary, []byte("new-binary"), 0700)
	os.WriteFile(target, []byte("old-binary"), 0700)
	p := &Publisher{Store: store, Workers: 2}
	r, err := p.UploadRelease(context.Background(), "v1", time.Now(), "commit", []BinFileInfo{{Path: binary, Platform: runtime.GOOS, Arch: runtime.GOARCH}})
	if err != nil || len(r.Files) != 1 {
		t.Fatal(r, err)
	}
	if _, err = p.UploadRelease(context.Background(), "v1", time.Now(), "", []BinFileInfo{{Path: binary}}); err == nil {
		t.Fatal("release overwritten")
	}
	u := &Updater{Store: store}
	info, err := u.Update(context.Background(), "v0", target)
	if err != nil || !info.Available {
		t.Fatal(info, err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "new-binary" {
		t.Fatal(string(b))
	}
	if err = Rollback(target); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(target)
	if string(b) != "old-binary" {
		t.Fatal(string(b))
	}
	store.mu.Lock()
	store.data[r.Files[0].Key] = []byte("bad-binary")
	store.mu.Unlock()
	if err = u.DownloadFile(context.Background(), r.Files[0], filepath.Join(root, "bad")); err == nil {
		t.Fatal("corrupt release accepted")
	}
}
