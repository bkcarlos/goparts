package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
)

// ChatStream invokes onChunk synchronously for each SSE data chunk. A callback
// error stops reading and is preserved in the error chain. Cancellation closes
// network reads; callbacks must themselves respect ctx. No automatic retries.
// Success requires [DONE]. Premature EOF returns io.ErrUnexpectedEOF.
func (c *Client) ChatStream(ctx context.Context, input ChatRequest, onChunk func(ChatChunk) error) error {
	if onChunk == nil {
		return errors.New("llm: stream callback is required")
	}
	body, err := c.chatBody(input, true)
	if err != nil {
		return err
	}
	resp, cancel, err := c.request(ctx, "/chat/completions", body, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer resp.Body.Close()
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		data, readErr := readBounded(resp.Body, c.maxResponseBytes)
		if readErr != nil {
			return readErr
		}
		if apiErr := parseAPIError(data, resp); apiErr != nil {
			return apiErr
		}
		return fmt.Errorf("%w: expected text/event-stream", ErrInvalidResponse)
	}
	reader := bufio.NewReader(resp.Body)
	var data strings.Builder
	size := 0
	seen := false
	for {
		if err := resp.Request.Context().Err(); err != nil {
			return err
		}
		line, err := readLine(reader, c.maxEventBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("llm: stream ended before [DONE]: %w", io.ErrUnexpectedEOF)
			}
			return fmt.Errorf("llm: read stream: %w", withoutURL(err))
		}
		if line == "" {
			payload := strings.TrimSuffix(data.String(), "\n")
			data.Reset()
			size = 0
			if payload == "" {
				continue
			}
			if strings.TrimSpace(payload) == "[DONE]" {
				if !seen {
					return ErrInvalidResponse
				}
				return nil
			}
			if apiErr := parseAPIError([]byte(payload), resp); apiErr != nil {
				return apiErr
			}
			var chunk ChatChunk
			if json.Unmarshal([]byte(payload), &chunk) != nil || (len(chunk.Choices) == 0 && chunk.Usage == nil) {
				return fmt.Errorf("%w: malformed stream chunk", ErrInvalidResponse)
			}
			chunk.RequestID = requestID(resp)
			seen = true
			if err := onChunk(chunk); err != nil {
				return fmt.Errorf("llm: stream callback: %w", err)
			}
			continue
		}
		size += len(line) + 1
		if size > c.maxEventBytes {
			return ErrEventTooLarge
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
}

func readLine(reader *bufio.Reader, limit int) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(part) > limit-len(line) {
			return "", ErrEventTooLarge
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"), nil
	}
}
