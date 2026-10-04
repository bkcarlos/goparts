package card

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSharedTokenCacheAcrossClients(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"code":0,"tenant_access_token":"cached","expire":7200}`)
	}))
	defer srv.Close()
	cache := &MemoryTokenCache{}
	a, _ := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, TokenCache: cache})
	b, _ := New(Config{AppID: "app", AppSecret: "secret", BaseURL: srv.URL, TokenCache: cache})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		client := a
		if i%2 == 0 {
			client = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := client.AccessToken(context.Background())
			if err != nil || token != "cached" {
				t.Error(token, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
