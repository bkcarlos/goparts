package card

import "strconv"

func (e *APIError) ErrorInfo() (string, string) {
	return "feishu.card.api_error", "Feishu card API request failed"
}
func (e *APIError) ErrorFields() map[string]string {
	return map[string]string{
		"http_status": strconv.Itoa(e.StatusCode), "upstream_code": strconv.Itoa(e.Code), "request_id": e.RequestID,
	}
}
