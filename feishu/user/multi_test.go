package user

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMultiUserIdentityIsolation(t *testing.T) {
	store := &MultiMemoryStore{}
	cfg := Config{AppID: "app", AppSecret: "secret", Scopes: []string{"docs:read"}}
	m, err := NewManager(cfg, store, func(context.Context) (string, error) { return "app-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	s, _ := store.ForUser("ou_a")
	token := Token{AppID: "app", Issuer: DefaultAccountsURL, APIBaseURL: DefaultBaseURL, OpenID: "ou_a", AccessToken: "user-token", TokenType: "Bearer", Scope: "docs:read offline_access", ExpiresAt: time.Now().Add(time.Hour)}
	if err = s.Save(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	actual, err := m.AccessToken(AsUser(context.Background(), "ou_a"))
	if err != nil || actual != "user-token" {
		t.Fatal(actual, err)
	}
	if _, err = m.AccessToken(AsUser(context.Background(), "ou_b")); !errors.Is(err, ErrLoginRequired) {
		t.Fatal(err)
	}
	if _, err = m.AccessToken(context.Background()); err == nil {
		t.Fatal("implicit app fallback")
	}
	actual, err = m.AccessToken(AsApplication(context.Background()))
	if err != nil || actual != "app-token" {
		t.Fatal(actual, err)
	}
	token.OpenID = "ou_b"
	if s.Save(context.Background(), token) == nil {
		t.Fatal("cross-account token accepted")
	}
}
func TestFileRefreshLockCancellation(t *testing.T) {
	lock := FileLocker{Path: filepath.Join(t.TempDir(), "refresh.lock"), PollInterval: time.Millisecond}
	unlock, err := lock.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err = lock.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	unlock()
	unlock2, err := lock.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}
