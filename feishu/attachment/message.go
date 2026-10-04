package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

type Receiver struct{ Type, ID string }

func Chat(id string) Receiver     { return Receiver{Type: "chat_id", ID: id} }
func User(openID string) Receiver { return Receiver{Type: "open_id", ID: openID} }

type SendOptions struct{ UUID string } // optional message deduplication key, at most 50 characters
type Message struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
}

// SendFile sends an IM file_key. Drive/media file_tokens are not interchangeable.
// Upload and send are separate so a send failure does not force another upload.
func (c *Client) SendFile(ctx context.Context, receiver Receiver, fileKey string, opts SendOptions) (Message, error) {
	switch receiver.Type {
	case "chat_id", "open_id", "union_id", "user_id", "email":
	default:
		return Message{}, errors.New("feishu/attachment: unsupported receiver type")
	}
	if strings.TrimSpace(receiver.ID) == "" || !validToken(fileKey) || utf8.RuneCountInString(opts.UUID) > 50 {
		return Message{}, errors.New("feishu/attachment: invalid receiver, file key or UUID")
	}
	content, _ := json.Marshal(map[string]string{"file_key": fileKey})
	input := struct {
		ReceiveID string `json:"receive_id"`
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
		UUID      string `json:"uuid,omitempty"`
	}{receiver.ID, "file", string(content), opts.UUID}
	data, err := json.Marshal(input)
	if err != nil {
		return Message{}, err
	}
	ctx, cancel, err := c.context(ctx)
	if err != nil {
		return Message{}, err
	}
	defer cancel()
	var result Message
	err = c.request(ctx, "/im/v1/messages?receive_id_type="+url.QueryEscape(receiver.Type), "application/json; charset=utf-8", bytes.NewReader(data), int64(len(data)), &result)
	if err == nil && result.MessageID == "" {
		err = ErrInvalidResponse
	}
	return result, err
}
