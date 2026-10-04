// Package redis is an optional Redis adapter for goparts/cache.Store. It does not
// own the supplied client's lifetime and adds no implicit distributed lock.
package redis

import (
	"context"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"time"
)

type Store struct {
	client   goredis.Cmdable
	prefix   string
	maxBytes int
}

func New(client goredis.Cmdable, prefix string, maxBytes int) (*Store, error) {
	if client == nil || maxBytes < 0 {
		return nil, errors.New("cache/redis: invalid client/limit")
	}
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	return &Store{client, prefix, maxBytes}, nil
}
func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(value) > s.maxBytes {
		return nil, false, errors.New("cache/redis: value exceeds limit")
	}
	return value, true, nil
}
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl < 0 || len(value) > s.maxBytes {
		return errors.New("cache/redis: invalid TTL/value size")
	}
	return s.client.Set(ctx, s.prefix+key, value, ttl).Err()
}
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.prefix+key).Err()
}
