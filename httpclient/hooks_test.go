package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafeHooksAndPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret")
		w.WriteHeader(400)
		w.Write([]byte("password=secret"))
	}))
	defer srv.Close()
	called := 0
	c, err := New(Config{ErrorBodyBytes: 8, RedactBody: func(b []byte) []byte { return []byte("redacted-long") }, Hooks: Hooks{OnRequest: func(_ context.Context, e RequestEvent) {
		if e.Headers.Get("Authorization") != "" {
			t.Error("leak")
		}
		e.Headers.Set("Accept", "changed")
		called++
	}, OnResponse: func(_ context.Context, e ResponseEvent) {
		if !e.Failed || e.StatusCode != 400 || e.Bytes != 15 || e.Headers.Get("Set-Cookie") != "" {
			t.Errorf("event %+v", e)
		}
		called++
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Do(context.Background(), Request{URL: srv.URL + "?token=secret", Headers: http.Header{"Authorization": {"Bearer secret"}}})
	var se *StatusError
	if !errors.As(err, &se) || string(se.BodyPreview) != "redacted" || called != 2 {
		t.Fatalf("%v %d", err, called)
	}
}
