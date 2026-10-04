package user

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrNoToken = errors.New("feishu/user: no stored token")
var ErrTokenStore = errors.New("feishu/user: token storage failed")

// Token contains secrets. String/GoString redact them; JSON is intended only
// for secure persistence. Do not log or transmit its JSON representation.
type Token struct {
	AppID            string    `json:"app_id"`
	Issuer           string    `json:"issuer"`
	APIBaseURL       string    `json:"api_base_url"`
	OpenID           string    `json:"open_id"`
	Name             string    `json:"name"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	Scope            string    `json:"scope"`
	ObtainedAt       time.Time `json:"obtained_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

func (t Token) String() string   { return "feishu/user: token (credentials redacted)" }
func (t Token) GoString() string { return t.String() }

// TokenStore stores a single account. Save must replace the record atomically.
// Reuse one Client per store/account. Multi-process deployments must additionally
// coordinate the entire refresh transaction, not merely individual store calls.
type TokenStore interface {
	Load(context.Context) (Token, error)
	Save(context.Context, Token) error
	Delete(context.Context) error
}

type MemoryStore struct {
	mu    sync.Mutex
	token *Token
}

func (s *MemoryStore) Load(ctx context.Context) (Token, error) {
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == nil {
		return Token{}, ErrNoToken
	}
	return *s.token, nil
}
func (s *MemoryStore) Save(ctx context.Context, t Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = &t
	return nil
}
func (s *MemoryStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = nil
	return nil
}

// EncryptedFileStore encrypts with AES-256-GCM and replaces a 0600 file atomically.
// The caller supplies and protects the key separately (for example in a secret
// manager). There is no plaintext fallback and no cross-process refresh lock.
type EncryptedFileStore struct {
	mu   sync.Mutex
	path string
	aead cipher.AEAD
}

const storeVersion = "FSU1"

var storeAAD = []byte("github.com/bkcarlos/goparts/feishu/user/token/v1")

func NewEncryptedFileStore(path string, key []byte) (*EncryptedFileStore, error) {
	if path == "" || len(key) != 32 {
		return nil, errors.New("feishu/user: token path and 32-byte encryption key are required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrTokenStore
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrTokenStore
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrTokenStore
	}
	return &EncryptedFileStore{path: absolute, aead: aead}, nil
}

func (s *EncryptedFileStore) Load(ctx context.Context) (Token, error) {
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Token{}, ErrNoToken
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return Token{}, ErrTokenStore
	}
	f, err := os.Open(s.path)
	if err != nil {
		return Token{}, ErrTokenStore
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Token{}, ErrTokenStore
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return Token{}, ErrTokenStore
	}
	n := s.aead.NonceSize()
	if len(data) < len(storeVersion)+n+s.aead.Overhead() || string(data[:len(storeVersion)]) != storeVersion {
		return Token{}, ErrTokenStore
	}
	nonce := data[len(storeVersion) : len(storeVersion)+n]
	plain, err := s.aead.Open(nil, nonce, data[len(storeVersion)+n:], storeAAD)
	if err != nil {
		return Token{}, ErrTokenStore
	}
	var token Token
	if json.Unmarshal(plain, &token) != nil {
		return Token{}, ErrTokenStore
	}
	return token, nil
}

func (s *EncryptedFileStore) Save(ctx context.Context, token Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plain, err := json.Marshal(token)
	if err != nil {
		return ErrTokenStore
	}
	if len(plain) > 1024*1024-len(storeVersion)-s.aead.NonceSize()-s.aead.Overhead() {
		return ErrTokenStore
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ErrTokenStore
	}
	data := append([]byte(storeVersion), nonce...)
	data = s.aead.Seal(data, nonce, plain, storeAAD)
	dir := filepath.Dir(s.path)
	if os.MkdirAll(dir, 0700) != nil {
		return ErrTokenStore
	}
	if info, err := os.Lstat(s.path); err == nil {
		if !info.Mode().IsRegular() {
			return ErrTokenStore
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrTokenStore
	}
	f, err := os.CreateTemp(dir, ".feishu-user-*")
	if err != nil {
		return ErrTokenStore
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if f.Chmod(0600) != nil {
		return ErrTokenStore
	}
	if _, err := f.Write(data); err != nil {
		return ErrTokenStore
	}
	if f.Sync() != nil || f.Close() != nil {
		return ErrTokenStore
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.Rename(f.Name(), s.path) != nil {
		return ErrTokenStore
	}
	return nil
}

func (s *EncryptedFileStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrTokenStore
	}
	return nil
}
