package user

import (
	"strconv"
	"strings"
)

func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.user.api_error", "Feishu user API request failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{
		"http_status": strconv.Itoa(e.StatusCode), "upstream_code": strconv.Itoa(e.Code), "request_id": e.RequestID,
	}
}
func (e *OAuthError) ErrorInfo() (string, string) {
	return "feishu.user.oauth_error", "Feishu user authorization failed"
}
func (e *OAuthError) ErrorFields() map[string]string {
	return map[string]string{
		"http_status": strconv.Itoa(e.StatusCode), "upstream_code": strconv.Itoa(e.Code), "oauth_error": e.ErrorCode,
	}
}
func (e *ScopeError) ErrorInfo() (string, string) {
	return "feishu.user.missing_scope", "Feishu user permission missing"
}
func (e *ScopeError) ErrorFields() map[string]string {
	return map[string]string{"missing_scopes": strings.Join(e.Missing, " ")}
}
