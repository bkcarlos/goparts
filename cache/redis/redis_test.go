package redis

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestRedisTTLAndPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	defer client.Close()
	store, _ := New(client, "prefix:", 100)
	ctx := context.Background()
	if err := store.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if !server.Exists("prefix:key") {
		t.Fatal("prefix")
	}
	v, ok, err := store.Get(ctx, "key")
	if err != nil || !ok || string(v) != "value" {
		t.Fatal(v, ok, err)
	}
	server.FastForward(2 * time.Minute)
	if _, ok, err = store.Get(ctx, "key"); err != nil || ok {
		t.Fatal(ok, err)
	}
}
