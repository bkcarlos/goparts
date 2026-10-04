package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bkcarlos/goparts/apperror"
	"github.com/bkcarlos/goparts/feishu"
	"github.com/bkcarlos/goparts/feishu/attachment"
	"github.com/bkcarlos/goparts/feishu/card"
	"github.com/bkcarlos/goparts/feishu/events"
	"github.com/bkcarlos/goparts/feishu/user"
	"github.com/bkcarlos/goparts/httpclient"
	"github.com/bkcarlos/goparts/llm"
)

// This workspace-only test keeps the production modules dependency-free.
func TestModuleErrorsCanBeReportedWithoutConversion(t *testing.T) {
	for _, tc := range []struct {
		err              error
		code, key, value string
	}{
		{&httpclient.StatusError{StatusCode: 503}, "httpclient.http_status", "http_status", "503"},
		{&llm.APIError{StatusCode: 429, Code: "rate_limit", Message: "provider-secret", RequestID: "req-1"}, "llm.api_error", "upstream_code", "rate_limit"},
		{&feishu.APIError{Code: 999, Message: "provider-secret"}, "feishu.webhook_api_error", "upstream_code", "999"},
		{&feishu.HTTPError{StatusCode: 502}, "feishu.http_status", "http_status", "502"},
		{&card.APIError{StatusCode: 400, Code: 200770, Message: "provider-secret", RequestID: "card-req"}, "feishu.card.api_error", "request_id", "card-req"},
		{&attachment.APIError{StatusCode: 403, Code: 1061004, Message: "provider-secret", RequestID: "upload-req"}, "feishu.attachment.api_error", "request_id", "upload-req"},
		{&events.APIError{StatusCode: 403, Code: 403, Message: "provider-secret", RequestID: "ws-req"}, "feishu.events.api_error", "request_id", "ws-req"},
		{&user.APIError{StatusCode: 403, Code: 999, Message: "provider-secret"}, "feishu.user.api_error", "http_status", "403"},
		{&user.OAuthError{StatusCode: 400, ErrorCode: "invalid_grant", Description: "provider-secret"}, "feishu.user.oauth_error", "oauth_error", "invalid_grant"},
		{&user.ScopeError{Missing: []string{"docx:document"}}, "feishu.user.missing_scope", "missing_scopes", "docx:document"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			calls := 0
			reporter, err := apperror.NewReporter(apperror.ReporterConfig{Sink: apperror.SinkFunc(func(ctx context.Context, r apperror.Record) error {
				calls++
				if r.Code != tc.code || r.Message == "" || r.Fields[tc.key] != tc.value {
					t.Fatalf("record=%+v", r)
				}
				body, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(body), "provider-secret") {
					t.Fatalf("raw upstream diagnostic leaked: %s", body)
				}
				return nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			originalText := tc.err.Error()
			if err = reporter.Capture(context.Background(), fmt.Errorf("boundary: %w", tc.err)); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || tc.err.Error() != originalText {
				t.Fatal("report changed original error or report count")
			}
			outer := apperror.Wrap(tc.err, "service.failed", "服务调用失败")
			if !errors.Is(outer, tc.err) || apperror.Describe(outer).Fields[tc.key] != tc.value {
				t.Fatal("wrapping lost the original error or fields")
			}
		})
	}
}
