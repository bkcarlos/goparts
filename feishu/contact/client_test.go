package contact_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/bkcarlos/goparts/feishu/contact"
)

func TestBatchIdentityAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input contact.BatchRequest
		kind  string
		valid bool
	}{
		{"default", contact.BatchRequest{Emails: []string{"a@example.com"}}, "open_id", true},
		{"union and resigned", contact.BatchRequest{Mobiles: []string{"123"}, UserIDType: "union_id", IncludeResigned: true}, "union_id", true},
		{"boundary", contact.BatchRequest{Emails: make([]string, 50), Mobiles: make([]string, 50), UserIDType: "user_id"}, "user_id", true},
		{"empty", contact.BatchRequest{}, "", false},
		{"too many emails", contact.BatchRequest{Emails: make([]string, 51)}, "", false},
		{"too many mobiles", contact.BatchRequest{Mobiles: make([]string, 51)}, "", false},
		{"invalid id type", contact.BatchRequest{Emails: []string{"a"}, UserIDType: "invalid"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/contact/v3/users/batch_get_id" || r.URL.Query().Get("user_id_type") != tc.kind || r.Header.Get("Authorization") != "Bearer user" {
					t.Errorf("request %s %s", r.Method, r.URL)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if _, ok := body["user_id_type"]; ok {
					t.Error("query-only field leaked into body")
				}
				raw, err := json.Marshal(body)
				if err != nil {
					t.Error(err)
				}
				var got contact.BatchRequest
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Error(err)
				}
				want := tc.input
				want.UserIDType = ""
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body=%+v want=%+v", got, want)
				}
				w.Write([]byte(`{"code":0,"data":{"user_list":[{"user_id":"ou_1","email":"a@example.com"}]}}`))
			}))
			defer srv.Close()
			c, err := contact.New(contact.Config{BaseURL: srv.URL, TokenProvider: func(context.Context) (string, error) { return "user", nil }})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.BatchGetID(context.Background(), tc.input)
			if tc.valid {
				if err != nil || len(got) != 1 || got[0].UserID != "ou_1" || calls.Load() != 1 {
					t.Fatalf("got=%v err=%v calls=%d", got, err, calls.Load())
				}
			} else if err == nil || calls.Load() != 0 {
				t.Fatalf("invalid request sent: err=%v calls=%d", err, calls.Load())
			}
		})
	}
}
