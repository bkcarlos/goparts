package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestContextFieldsLevelAndSource(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	l, err := New(Config{Writer: &out, Service: "orders", Environment: "test", Level: &level, AddSource: true})
	if err != nil {
		t.Fatal(err)
	}
	parent := WithContext(context.Background(), l)
	child := WithFields(parent, "request_id", "r1")
	FromContext(child).DebugContext(child, "hidden")
	if out.Len() != 0 {
		t.Fatal("debug was not filtered")
	}
	level.Set(slog.LevelDebug)
	FromContext(child).WithGroup("db").DebugContext(child, "query", "rows", 3)
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["request_id"] != "r1" || got["service"] != "orders" || got["environment"] != "test" || got["level"] != "DEBUG" {
		t.Fatalf("fields: %v", got)
	}
	if got["db"].(map[string]any)["rows"] != float64(3) {
		t.Fatalf("group: %v", got)
	}
	if !strings.HasSuffix(got["source"].(map[string]any)["file"].(string), "logger_test.go") {
		t.Fatalf("wrong source: %v", got["source"])
	}
	out.Reset()
	FromContext(parent).InfoContext(parent, "parent")
	if strings.Contains(out.String(), "request_id") {
		t.Fatal("fields leaked into parent")
	}
}

func TestConcurrentLogging(t *testing.T) {
	var out bytes.Buffer
	l, err := New(Config{Writer: &out})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); l.With("worker", i).Info("done") }(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("corrupt line: %s", line)
		}
	}
}

func TestConfigurationAndFallback(t *testing.T) {
	if _, err := New(Config{Format: "invalid"}); err == nil {
		t.Fatal("invalid format accepted")
	}
	var out bytes.Buffer
	l, err := New(Config{Format: "text", Writer: &out})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello")
	if !strings.Contains(out.String(), "msg=hello") {
		t.Fatalf("text format: %s", out.String())
	}
	if FromContext(nil) != slog.Default() || FromContext(context.Background()) != slog.Default() || FromContext(WithContext(context.Background(), nil)) != slog.Default() {
		t.Fatal("incorrect fallback")
	}
}
