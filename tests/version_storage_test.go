package integration_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/bkcarlos/goparts/storage"
	"github.com/bkcarlos/goparts/version"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type releaseBackend struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (b *releaseBackend) Put(_ context.Context, k string, r io.Reader, n int64, _ storage.PutOptions) (storage.Object, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return storage.Object{}, err
	}
	b.mu.Lock()
	b.values[k] = data
	b.mu.Unlock()
	return storage.Object{Key: k, Size: n}, nil
}
func (b *releaseBackend) Get(_ context.Context, k string, _ storage.GetOptions) (*storage.Reader, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.values[k]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.Reader{ReadCloser: io.NopCloser(bytes.NewReader(data)), Object: storage.Object{Key: k, Size: int64(len(data))}, Length: int64(len(data))}, nil
}
func (b *releaseBackend) Stat(context.Context, string) (storage.Object, error) {
	return storage.Object{}, storage.ErrUnsupported
}
func (b *releaseBackend) Delete(context.Context, string) error { return storage.ErrUnsupported }
func (b *releaseBackend) List(context.Context, storage.ListOptions) (storage.Page, error) {
	return storage.Page{}, storage.ErrUnsupported
}
func (b *releaseBackend) PresignGet(context.Context, string, time.Duration) (storage.SignedURL, error) {
	return storage.SignedURL{}, storage.ErrUnsupported
}
func TestVersionUsesProviderNeutralStore(t *testing.T) {
	objects, _ := storage.New(&releaseBackend{values: map[string][]byte{}}, storage.Config{BasePath: "app"})
	adapter := version.StorageAdapter{PutFunc: func(ctx context.Context, key string, r io.Reader, n int64) error {
		_, err := objects.Put(ctx, key, r, n, storage.PutOptions{})
		return err
	}, OpenFunc: func(ctx context.Context, key string) (io.ReadCloser, error) {
		r, err := objects.Get(ctx, key, storage.GetOptions{})
		if errors.Is(err, storage.ErrNotFound) {
			return nil, version.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return r, nil
	}}
	p := &version.Publisher{Store: adapter}
	root := t.TempDir()
	binary := filepath.Join(root, "binary")
	os.WriteFile(binary, []byte("portable"), 0700)
	_, err := p.UploadRelease(context.Background(), "v1", time.Now(), "", []version.BinFileInfo{{Path: binary}})
	if err != nil {
		t.Fatal(err)
	}
	u := &version.Updater{Store: adapter}
	dst := filepath.Join(root, "download")
	if _, err = u.DownloadPlatform(context.Background(), "v1", "", "", dst); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "portable" {
		t.Fatal(string(data))
	}
}
