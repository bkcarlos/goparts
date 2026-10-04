package logger

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactionNestedAndWith(t *testing.T) {
	var b bytes.Buffer
	l, _ := New(Config{Writer: &b, Redact: true})
	l.With("authorization", "Bearer secret1").WithGroup("request").Info("Bearer secret2", slog.Any("payload", map[string]any{"api_key": "secret3", "nested": []any{map[string]any{"refresh_token": "secret4"}}}), slog.Group("group", slog.String("password", "secret5")))
	for _, secret := range []string{"secret1", "secret2", "secret3", "secret4", "secret5"} {
		if strings.Contains(b.String(), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if !strings.Contains(b.String(), "REDACTED") {
		t.Fatal(b.String())
	}
}
func TestRotation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	w, err := NewRotatingWriter(FileConfig{Path: p, MaxBytes: 5, Backups: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"1111", "2222", "3333"} {
		if _, err = w.Write([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	for path, want := range map[string]string{p: "3333", p + ".1": "2222", p + ".2": "1111"} {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != want {
			t.Fatalf("%s: %s %v", path, b, err)
		}
	}
	if _, err = w.Write(nil); err == nil {
		t.Fatal("write after close")
	}
}
