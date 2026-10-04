package card

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type CachedToken struct {
	Value     string
	ExpiresAt time.Time
}

func (t CachedToken) String() string   { return "card token (redacted)" }
func (t CachedToken) GoString() string { return t.String() }

// TokenCache coordinates the whole refresh transaction, not just Get/Set.
// Implementations must not return expired tokens and must not cache errors.
type TokenCache interface {
	GetOrLoad(context.Context, string, func(context.Context) (CachedToken, error)) (CachedToken, error)
}
type tokenSlot struct {
	gate  chan struct{}
	token CachedToken
}
type MemoryTokenCache struct {
	mu    sync.Mutex
	slots map[string]*tokenSlot
}

func (m *MemoryTokenCache) GetOrLoad(ctx context.Context, key string, load func(context.Context) (CachedToken, error)) (CachedToken, error) {
	m.mu.Lock()
	if m.slots == nil {
		m.slots = map[string]*tokenSlot{}
	}
	slot := m.slots[key]
	if slot == nil {
		slot = &tokenSlot{gate: make(chan struct{}, 1)}
		m.slots[key] = slot
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return CachedToken{}, ctx.Err()
	case slot.gate <- struct{}{}:
	}
	defer func() { <-slot.gate }()
	if slot.token.Value != "" && time.Now().Before(slot.token.ExpiresAt) {
		return slot.token, nil
	}
	t, err := load(ctx)
	if err != nil {
		return CachedToken{}, err
	}
	if t.Value == "" || !time.Now().Before(t.ExpiresAt) {
		return CachedToken{}, errors.New("card: invalid cached token")
	}
	slot.token = t
	return t, nil
}
func (c *Client) cacheKey() string {
	sum := sha256.Sum256([]byte(c.baseURL + "\x00" + c.appID + "\x00" + c.appSecret))
	return hex.EncodeToString(sum[:])
}
