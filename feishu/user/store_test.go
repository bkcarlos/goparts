package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedFileStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "session.enc")
	key := bytes.Repeat([]byte{7}, 32)
	s, err := NewEncryptedFileStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx); !errors.Is(err, ErrNoToken) {
		t.Fatal(err)
	}
	token := Token{AppID: "app", AccessToken: "access-secret", RefreshToken: "refresh-secret"}
	if err := s.Save(ctx, token); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("access-secret")) || bytes.Contains(data, []byte("refresh-secret")) {
		t.Fatal("plaintext credentials on disk")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file permissions=%v", info.Mode())
	}
	reopened, _ := NewEncryptedFileStore(path, key)
	loaded, err := reopened.Load(ctx)
	if err != nil || loaded.AccessToken != token.AccessToken {
		t.Fatalf("load: %v", err)
	}
	if fmt.Sprintf("%+v", loaded) == "" || bytes.Contains([]byte(fmt.Sprintf("%v %+v %#v", loaded, loaded, loaded)), []byte("access-secret")) {
		t.Fatal("token formatting leaked")
	}
	wrong, _ := NewEncryptedFileStore(path, bytes.Repeat([]byte{8}, 32))
	if _, err := wrong.Load(ctx); !errors.Is(err, ErrTokenStore) {
		t.Fatal("wrong key accepted")
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx); !errors.Is(err, ErrTokenStore) {
		t.Fatal("tampering accepted")
	}
	if err := s.Save(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx); !errors.Is(err, ErrTokenStore) {
		t.Fatal("insecure mode accepted")
	}
	if err := s.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx); !errors.Is(err, ErrNoToken) {
		t.Fatal(err)
	}
}

func TestStoreValidationAndSymlinks(t *testing.T) {
	if _, err := NewEncryptedFileStore("", make([]byte, 32)); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := NewEncryptedFileStore("file", make([]byte, 16)); err == nil {
		t.Fatal("weak key length accepted")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip(err)
	}
	s, _ := NewEncryptedFileStore(path, make([]byte, 32))
	if err := s.Save(context.Background(), Token{}); !errors.Is(err, ErrTokenStore) {
		t.Fatal("symlink write accepted")
	}
	if _, err := s.Load(context.Background()); !errors.Is(err, ErrTokenStore) {
		t.Fatal("symlink read accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, store := range []TokenStore{s, &MemoryStore{}} {
		if store.Save(ctx, Token{}) != context.Canceled {
			t.Fatal("save ignored cancellation")
		}
		if _, err := store.Load(ctx); err != context.Canceled {
			t.Fatal("load ignored cancellation")
		}
		if store.Delete(ctx) != context.Canceled {
			t.Fatal("delete ignored cancellation")
		}
	}
}
