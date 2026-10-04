package llm

import "strconv"

// Provider messages are deliberately excluded from reportable fields, as they
// may echo prompts or credentials. Raw diagnostics remain on APIError.
func (e *APIError) ErrorInfo() (string, string) {
	return "llm.api_error", "LLM API request failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{
		"http_status": strconv.Itoa(e.StatusCode), "request_id": e.RequestID,
		"upstream_code": e.Code, "upstream_type": e.Type, "retry_after": e.RetryAfter,
	}
}
