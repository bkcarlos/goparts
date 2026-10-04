package httpclient

import "strconv"

// ErrorInfo and ErrorFields satisfy apperror's structural reporting interfaces
// without introducing a dependency on that module.
func (e *StatusError) ErrorInfo() (string, string) {
	return "httpclient.http_status", "HTTP response status rejected"
}
func (e *StatusError) ErrorFields() map[string]string {
	return map[string]string{"http_status": strconv.Itoa(e.StatusCode)}
}
