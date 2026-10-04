package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChainAuthRecoveryAndAccess(t *testing.T) {
	var event AccessEvent
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ID(r.Context()) == "" {
			t.Error("missing ID")
		}
		panic("secret")
	}), RequestID(false), AccessLog(nil, func(e AccessEvent) { event = e }), Recover(nil), AuthBearer(StaticToken("token")))
	r := httptest.NewRequest("GET", "/?secret=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") || event.Status != 500 || event.RequestID == "" {
		t.Fatal(w.Code, event)
	}
}
func TestLimitsAndCORS(t *testing.T) {
	h := BodyLimit(2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", 413)
		}
	}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("abc"))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
	h = CORS(CORSConfig{Origins: []string{"https://example.com"}, Methods: []string{"POST"}, Headers: []string{"Content-Type"}})(http.NotFoundHandler())
	r = httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "DELETE")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r.Header.Set("Access-Control-Request-Method", "POST")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}
func TestTimeout(t *testing.T) {
	done := make(chan struct{})
	h := Timeout(time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(done) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	<-done
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	if StaticToken("")(context.Background(), "") {
		t.Fatal("empty token")
	}
}
