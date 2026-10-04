package user

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPassphraseStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	salt := []byte("random-test-salt!")
	a, err := NewPassphraseFileStore(path, []byte("long-test-passphrase"), salt, 600000)
	if err != nil {
		t.Fatal(err)
	}
	a.Save(context.Background(), Token{OpenID: "user", AccessToken: "secret"})
	b, err := NewPassphraseFileStore(path, []byte("long-test-passphrase"), salt, 600000)
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Load(context.Background())
	if err != nil || got.AccessToken != "secret" {
		t.Fatal(err)
	}
	if _, err = NewPassphraseFileStore(path, []byte("short"), salt, 600000); err == nil {
		t.Fatal("weak password")
	}
}
