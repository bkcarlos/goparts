package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testConfig(endpoint string) Config {
	return Config{AppID: "app", AppSecret: "secret", Scopes: []string{ScopeWriteDocuments}, BaseURL: endpoint + "/open-apis", AccountsURL: endpoint}
}
func activeToken(c *Client) Token {
	return Token{AppID: c.appID, Issuer: c.accountsURL, APIBaseURL: c.baseURL, OpenID: "ou_user", Name: "Test", TokenType: "Bearer", AccessToken: "user-access", RefreshToken: "refresh-old", Scope: strings.Join(c.scopes, " "), ObtainedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour)}
}
func save(t *testing.T, c *Client, token Token) {
	t.Helper()
	if err := c.store.Save(context.Background(), token); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceLoginProtocol(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/v1/device_authorization":
			id, secret, ok := r.BasicAuth()
			_ = r.ParseForm()
			if !ok || id != "app" || secret != "secret" || r.Form.Get("scope") != "docx:document offline_access" || r.Form.Get("client_id") != "app" {
				t.Errorf("invalid device request")
			}
			io.WriteString(w, `{"device_code":"private-code","user_code":"ABCD","verification_uri":"https://accounts.feishu.cn/device","verification_uri_complete":"https://accounts.feishu.cn/device?code=ABCD","expires_in":240,"interval":1}`)
		case "/oauth/v3/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.Form.Get("device_code") != "private-code" || r.Form.Get("client_secret") != "secret" {
				t.Error("invalid token request")
			}
			switch polls.Add(1) {
			case 1:
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"authorization_pending"}`)
			case 2:
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"slow_down"}`)
			default:
				io.WriteString(w, `{"access_token":"user-access","refresh_token":"refresh-old","expires_in":7200,"refresh_token_expires_in":604800,"scope":"docx:document offline_access","token_type":"Bearer"}`)
			}
		case "/open-apis/authen/v1/user_info":
			if r.Header.Get("Authorization") != "Bearer user-access" {
				t.Error("not user identity")
			}
			io.WriteString(w, `{"code":0,"data":{"open_id":"ou_user","name":"Test"}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	var waits []time.Duration
	c.wait = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() }
	auth, err := c.StartLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.UserCode != "ABCD" || strings.Contains(fmt.Sprintf("%+v %#v", auth, auth), "private-code") {
		t.Fatal("device code leaked")
	}
	info, err := c.CompleteLogin(context.Background(), auth)
	if err != nil || info.OpenID != "ou_user" {
		t.Fatalf("identity=%+v error=%v", info, err)
	}
	if len(waits) != 3 || waits[0] != time.Second || waits[1] != time.Second || waits[2] != 6*time.Second {
		t.Fatalf("poll intervals=%v", waits)
	}
	token, err := c.store.Load(context.Background())
	if err != nil || token.RefreshToken != "refresh-old" || token.AppID != "app" || token.OpenID != "ou_user" || token.ExpiresAt.Sub(token.ObtainedAt) != 7200*time.Second {
		t.Fatalf("stored token invalid: %v", err)
	}
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AccessToken(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Fatal(err)
	}
}

func TestLoginDenialExpiryAndScopes(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		target      error
	}{
		{"denied", `{"error":"access_denied","error_description":"secret-value"}`, nil},
		{"expired", `{"error":"expired_token"}`, ErrDeviceExpired},
		{"missing scopes", `{"access_token":"a","expires_in":3600,"scope":"offline_access"}`, nil},
		{"DPoP", `{"access_token":"a","expires_in":3600,"token_type":"DPoP"}`, ErrUnsupportedToken},
		{"missing lifetime", `{"access_token":"a"}`, ErrInvalidResponse},
		{"malformed", `not-json`, ErrInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.reply) }))
			defer srv.Close()
			c := mustClient(t, testConfig(srv.URL))
			c.wait = func(context.Context, time.Duration) error { return nil }
			auth := DeviceAuthorization{deviceCode: "device", appID: c.appID, issuer: c.accountsURL, ExpiresAt: time.Now().Add(time.Minute), Interval: time.Second}
			_, err := c.CompleteLogin(context.Background(), auth)
			if err == nil || (tc.target != nil && !errors.Is(err, tc.target)) || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("error=%v", err)
			}
			if _, err := c.store.Load(context.Background()); !errors.Is(err, ErrNoToken) {
				t.Fatal("failed login saved")
			}
		})
	}
	c := mustClient(t, testConfig("https://example.com"))
	auth := DeviceAuthorization{deviceCode: "device", appID: c.appID, issuer: c.accountsURL, ExpiresAt: time.Now().Add(-time.Second), Interval: time.Second}
	if _, err := c.CompleteLogin(context.Background(), auth); !errors.Is(err, ErrDeviceExpired) {
		t.Fatal(err)
	}
	auth.ExpiresAt = time.Now().Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.CompleteLogin(ctx, auth); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	auth.appID = "another-app"
	if _, err := c.CompleteLogin(context.Background(), auth); err == nil {
		t.Fatal("mismatched login accepted")
	}
}

func TestConcurrentRefreshAndRotation(t *testing.T) {
	var refreshes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/v3/token" {
			refreshes.Add(1)
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["grant_type"] != "refresh_token" || payload["refresh_token"] != "refresh-old" || payload["client_secret"] != "secret" {
				t.Error("invalid refresh payload")
			}
			io.WriteString(w, `{"code":0,"access_token":"fresh-access","refresh_token":"refresh-new","expires_in":7200,"refresh_token_expires_in":86400,"token_type":"Bearer"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh-access" {
			t.Error("stale/non-user token used")
		}
		io.WriteString(w, `{"code":0,"data":{"content":"document"}}`)
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	token := activeToken(c)
	token.ExpiresAt = time.Now().Add(-time.Second)
	save(t, c, token)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, err := c.ReadDocument(context.Background(), "doc1")
			if err != nil || text != "document" {
				t.Errorf("text=%s error=%v", text, err)
			}
		}()
	}
	wg.Wait()
	stored, err := c.store.Load(context.Background())
	if err != nil || stored.RefreshToken != "refresh-new" || stored.OpenID != "ou_user" || stored.Scope != token.Scope || refreshes.Load() != 1 {
		t.Fatalf("rotation error=%v refreshes=%d", err, refreshes.Load())
	}
}

type failingStore struct {
	MemoryStore
	fail bool
}

func (s *failingStore) Save(ctx context.Context, t Token) error {
	if s.fail {
		return ErrTokenStore
	}
	return s.MemoryStore.Save(ctx, t)
}

func TestRefreshPersistenceFailureRetainsNewToken(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}`)
	}))
	defer srv.Close()
	store := &failingStore{}
	cfg := testConfig(srv.URL)
	cfg.Store = store
	c := mustClient(t, cfg)
	token := activeToken(c)
	token.ExpiresAt = time.Now().Add(-time.Second)
	save(t, c, token)
	store.fail = true
	if _, err := c.AccessToken(context.Background()); !errors.Is(err, ErrTokenStore) {
		t.Fatal(err)
	}
	store.fail = false
	value, err := c.AccessToken(context.Background())
	if err != nil || value != "fresh" || calls.Load() != 1 {
		t.Fatalf("token persistence retried refresh: %v calls=%d", err, calls.Load())
	}
	stored, _ := store.Load(context.Background())
	if stored.RefreshToken != "rotated" {
		t.Fatal("rotated credential lost")
	}
}

func TestRefreshSurvivesCallerCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, `{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}`)
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	token := activeToken(c)
	token.ExpiresAt = time.Now().Add(-time.Second)
	save(t, c, token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.AccessToken(ctx); done <- err }()
	<-started
	cancel()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, _ := c.store.Load(context.Background())
	if stored.RefreshToken != "rotated" {
		t.Fatal("refresh lost on cancellation")
	}
}

func TestSessionValidationAndRefreshFailurePreservation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"temporarily_unavailable"}`)
	}))
	defer srv.Close()
	c := mustClient(t, testConfig(srv.URL))
	token := activeToken(c)
	token.ExpiresAt = time.Now().Add(-time.Second)
	save(t, c, token)
	if _, err := c.AccessToken(context.Background()); err == nil {
		t.Fatal("refresh failure ignored")
	}
	stored, _ := c.store.Load(context.Background())
	if stored.RefreshToken != "refresh-old" {
		t.Fatal("old token erased on transient error")
	}
	for _, mutate := range []func(*Token){func(t *Token) { t.AppID = "other" }, func(t *Token) { t.Issuer = "https://other" }, func(t *Token) { t.APIBaseURL = "https://other" }, func(t *Token) { t.TokenType = "DPoP" }, func(t *Token) { t.Scope = "offline_access" }, func(t *Token) { t.ExpiresAt = time.Now().Add(-time.Second); t.RefreshToken = "" }, func(t *Token) {
		t.ExpiresAt = time.Now().Add(-time.Second)
		t.RefreshExpiresAt = time.Now().Add(-time.Second)
	}} {
		token := activeToken(c)
		mutate(&token)
		save(t, c, token)
		if _, err := c.AccessToken(context.Background()); err == nil {
			t.Fatal("invalid session accepted")
		}
	}
	if _, err := c.AccessToken(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
