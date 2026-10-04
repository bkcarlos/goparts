package aliyun

import (
	"errors"
	"io"
	"net/http"
)

var ErrResponseTooLarge = errors.New("storage/aliyun: metadata response exceeds size limit")

type metadataTransport struct {
	next  http.RoundTripper
	limit int64
}

func (t *metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	objectBody := r.Method == http.MethodGet && !r.URL.Query().Has("list-type") && response.StatusCode >= 200 && response.StatusCode < 300
	if !objectBody {
		response.Body = &boundedBody{ReadCloser: response.Body, remaining: t.limit}
	}
	return response, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	if int64(n) > b.remaining {
		return 0, ErrResponseTooLarge
	}
	b.remaining -= int64(n)
	return n, err
}
