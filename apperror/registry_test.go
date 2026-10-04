package apperror

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

func TestRegistryJoinAndAttrs(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Definition{Code: "auth.denied", Message: "denied", Guidance: "sign in", Mapping: Mapping{HTTP: 401, Exit: 2}}); err != nil {
		t.Fatal(err)
	}
	a, _ := r.New("auth.denied", WithAttrs(slog.Int("attempts", 2), slog.Group("request", slog.String("id", "one"))))
	wrapped := fmt.Errorf("boundary: %w", a)
	if r.HTTPStatus(wrapped) != 401 || r.ExitCode(wrapped) != 2 || r.HTTPStatus(nil) != 200 || r.ExitCode(nil) != 0 {
		t.Fatal("mapping")
	}
	if a.ErrorFields()["guidance"] != "sign in" {
		t.Fatal("guidance")
	}
	record := Describe(a)
	record.Attrs["request"].(map[string]any)["id"] = "changed"
	if Describe(a).Attrs["request"].(map[string]any)["id"] != "one" {
		t.Fatal("attribute snapshot aliased")
	}
	all := DescribeAll(fmt.Errorf("joined: %w", errors.Join(a, New("write.failed", "failed"))))
	if len(all) != 2 || all[0].Code != "auth.denied" || all[1].Code != "write.failed" {
		t.Fatalf("branches=%v", all)
	}
	if err := r.Register(Definition{Code: "auth.denied", Message: "duplicate"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
