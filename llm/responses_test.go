package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponsesAndSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Error(r.URL.Path)
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["previous_response_id"] != "prior" || req["model"] != "test-model" {
			t.Error(req)
		}
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\"}}\n\n")
			return
		}
		io.WriteString(w, `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`)
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL, Model: "test-model"})
	req := ResponsesRequest{Input: "hi", PreviousResponseID: "prior", Reasoning: &Reasoning{Effort: "low"}}
	result, err := c.Responses(context.Background(), req)
	if err != nil || result.Text() != "hello" {
		t.Fatal(result, err)
	}
	var got string
	err = c.ResponsesStream(context.Background(), req, func(e ResponseEvent) error { got += e.Delta; return nil })
	if err != nil || got != "hello" {
		t.Fatal(got, err)
	}
}
func TestResponsesTerminalFailures(t *testing.T) {
	for _, tc := range []struct {
		data   string
		target error
		failed bool
	}{{`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n", io.ErrUnexpectedEOF, false}, {`data: {"type":"response.incomplete","response":{"id":"r","status":"incomplete"}}` + "\n\n", nil, true}, {`data: {"type":"response.completed"}` + "\n\n", ErrInvalidResponse, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, tc.data)
		}))
		c, _ := New(Config{BaseURL: srv.URL, Model: "m"})
		err := c.ResponsesStream(context.Background(), ResponsesRequest{Input: "hi"}, func(ResponseEvent) error { return nil })
		srv.Close()
		var status *ResponseStatusError
		if tc.failed {
			if !errors.As(err, &status) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, tc.target) {
			t.Fatal(err)
		}
	}
}
