package card

import (
	"context"
	"errors"
	"github.com/bkcarlos/goparts/feishu/dedup"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPCallbackDedupReplaysSuccess(t *testing.T) {
	cache, _ := dedup.NewMemory(time.Minute, 10)
	decoder, err := NewCallbackDecoder(CallbackConfig{AppID: "app", VerificationToken: "verify", Deduper: cache})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := decoder.Handler(func(context.Context, *CallbackRequest) (*CallbackResponse, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary")
		}
		return &CallbackResponse{}, nil
	})
	body := `{"schema":"2.0","header":{"event_id":"event","event_type":"card.action.trigger","app_id":"app","token":"verify"},"event":{"action":{"tag":"button"}}}`
	for _, want := range []int{500, 200, 200} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
