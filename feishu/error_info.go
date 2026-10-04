package feishu

import "strconv"

func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.webhook_api_error", "Feishu webhook request failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{"upstream_code": strconv.Itoa(e.Code)}
}
func (e *HTTPError) ErrorInfo() (string, string) {
	return "feishu.http_status", "Feishu HTTP response status rejected"
}
func (e *HTTPError) ErrorFields() map[string]string {
	return map[string]string{"http_status": strconv.Itoa(e.StatusCode)}
}
