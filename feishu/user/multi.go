package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PollTick struct {
	Attempt   int
	Interval  time.Duration
	ExpiresAt time.Time
}

// Locker protects Load -> refresh -> Save as one transaction across clients.
type Locker interface {
	Lock(context.Context) (func(), error)
}
type FileLocker struct {
	Path         string
	PollInterval time.Duration
}

// FileLocker uses exclusive creation. It never guesses that an existing lock is
// stale: after a process crash, an operator must remove its orphaned lock file.
func (l FileLocker) Lock(ctx context.Context) (func(), error) {
	if l.Path == "" || l.PollInterval < 0 {
		return nil, errors.New("user: invalid lock config")
	}
	interval := l.PollInterval
	if interval == 0 {
		interval = 50 * time.Millisecond
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0700); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.Close()
			var once sync.Once
			return func() { once.Do(func() { os.Remove(l.Path) }) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if err = waitContext(ctx, interval); err != nil {
			return nil, err
		}
	}
}

type MultiUserStore interface {
	ForUser(openID string) (TokenStore, error)
}
type MultiMemoryStore struct {
	mu     sync.Mutex
	stores map[string]*MemoryStore
}

func (m *MultiMemoryStore) ForUser(id string) (TokenStore, error) {
	if id == "" {
		return nil, errors.New("user: open ID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stores == nil {
		m.stores = map[string]*MemoryStore{}
	}
	if m.stores[id] == nil {
		m.stores[id] = &MemoryStore{}
	}
	return boundStore{m.stores[id], id}, nil
}

type MultiEncryptedStore struct {
	dir string
	key []byte
}

func NewMultiEncryptedStore(dir string, key []byte) (*MultiEncryptedStore, error) {
	if dir == "" || len(key) != 32 {
		return nil, errors.New("user: directory and 32-byte key required")
	}
	return &MultiEncryptedStore{dir, append([]byte(nil), key...)}, nil
}
func accountName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
func (m *MultiEncryptedStore) ForUser(id string) (TokenStore, error) {
	if id == "" {
		return nil, errors.New("user: open ID required")
	}
	store, err := NewEncryptedFileStore(filepath.Join(m.dir, accountName(id)+".token"), m.key)
	if err != nil {
		return nil, err
	}
	return boundStore{store, id}, nil
}
func (m *MultiEncryptedStore) Locker(id string) Locker {
	return FileLocker{Path: filepath.Join(m.dir, accountName(id)+".lock")}
}

type boundStore struct {
	TokenStore
	id string
}

func (s boundStore) Load(ctx context.Context) (Token, error) {
	t, err := s.TokenStore.Load(ctx)
	if err == nil && t.OpenID != s.id {
		return Token{}, errors.New("user: account mismatch")
	}
	return t, err
}
func (s boundStore) Save(ctx context.Context, t Token) error {
	if t.OpenID != s.id {
		return errors.New("user: account mismatch")
	}
	return s.TokenStore.Save(ctx, t)
}

type identityKey struct{}

// AsUser explicitly selects a user. Empty identity is rejected by AccessToken;
// it never changes into application identity as a fallback.
func AsUser(ctx context.Context, openID string) context.Context {
	return context.WithValue(ctx, identityKey{}, openID)
}
func AsApplication(ctx context.Context) context.Context {
	return context.WithValue(ctx, identityKey{}, applicationIdentity{})
}

type applicationIdentity struct{}
type Manager struct {
	mu      sync.Mutex
	cfg     Config
	store   MultiUserStore
	clients map[string]*Client
	app     func(context.Context) (string, error)
}

func NewManager(cfg Config, store MultiUserStore, applicationToken func(context.Context) (string, error)) (*Manager, error) {
	if store == nil {
		return nil, errors.New("user: multi-user store required")
	}
	if _, err := New(cfg); err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, store: store, clients: map[string]*Client{}, app: applicationToken}, nil
}
func (m *Manager) User(id string) (*Client, error) {
	if id == "" {
		return nil, errors.New("user: open ID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[id]; c != nil {
		return c, nil
	}
	store, err := m.store.ForUser(id)
	if err != nil {
		return nil, err
	}
	cfg := m.cfg
	cfg.Store = store
	if lockers, ok := m.store.(interface{ Locker(string) Locker }); ok {
		cfg.RefreshLocker = lockers.Locker(id)
	}
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	m.clients[id] = c
	return c, nil
}
func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("user: context required")
	}
	switch identity := ctx.Value(identityKey{}).(type) {
	case string:
		c, err := m.User(identity)
		if err != nil {
			return "", err
		}
		return c.AccessToken(ctx)
	case applicationIdentity:
		if m.app != nil {
			return m.app(ctx)
		}
	}
	return "", errors.New("user: explicit user/application identity required")
}
