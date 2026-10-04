package card

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Receiver struct{ Type, ID string }

func Chat(chatID string) Receiver { return Receiver{Type: "chat_id", ID: chatID} }
func User(openID string) Receiver { return Receiver{Type: "open_id", ID: openID} }

type Message struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
	RootID    string `json:"root_id"`
	ParentID  string `json:"parent_id"`
}

type SendOptions struct{ UUID string }
type ReplyOptions struct {
	UUID     string
	InThread bool
}

func (c *Client) Send(ctx context.Context, receiver Receiver, content any, opts SendOptions) (Message, error) {
	switch receiver.Type {
	case "chat_id", "open_id", "union_id", "user_id", "email":
	default:
		return Message{}, errors.New("feishu/card: unsupported receiver type")
	}
	if strings.TrimSpace(receiver.ID) == "" || utf8.RuneCountInString(opts.UUID) > 50 {
		return Message{}, errors.New("feishu/card: invalid receiver or UUID")
	}
	encoded, err := encodeContent(content)
	if err != nil {
		return Message{}, err
	}
	input := struct {
		ReceiveID string `json:"receive_id"`
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
		UUID      string `json:"uuid,omitempty"`
	}{receiver.ID, "interactive", encoded, opts.UUID}
	return c.sendMessage(ctx, "/im/v1/messages?receive_id_type="+url.QueryEscape(receiver.Type), input)
}

func (c *Client) Reply(ctx context.Context, messageID string, content any, opts ReplyOptions) (Message, error) {
	if !validID(messageID) || utf8.RuneCountInString(opts.UUID) > 50 {
		return Message{}, errors.New("feishu/card: invalid message ID or UUID")
	}
	encoded, err := encodeContent(content)
	if err != nil {
		return Message{}, err
	}
	input := struct {
		MsgType  string `json:"msg_type"`
		Content  string `json:"content"`
		UUID     string `json:"uuid,omitempty"`
		InThread bool   `json:"reply_in_thread,omitempty"`
	}{"interactive", encoded, opts.UUID, opts.InThread}
	return c.sendMessage(ctx, "/im/v1/messages/"+messageID+"/reply", input)
}

func (c *Client) sendMessage(ctx context.Context, path string, input any) (Message, error) {
	var result Message
	err := c.api(ctx, "POST", path, input, &result)
	if err == nil && result.MessageID == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

// UpdateMessage updates a shared card within 14 days, using its original sender.
// Both the original and replacement card must explicitly enable update_multi.
func (c *Client) UpdateMessage(ctx context.Context, messageID string, content any) error {
	if !validID(messageID) {
		return errors.New("feishu/card: invalid message ID")
	}
	encoded, err := encodeContent(content)
	if err != nil {
		return err
	}
	return c.api(ctx, "PATCH", "/im/v1/messages/"+messageID, map[string]string{"content": encoded}, nil)
}

type cardData struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// Create creates a JSON 2.0 Cardkit entity. Send Reference(id) to deliver it once.
func (c *Client) Create(ctx context.Context, content any) (string, error) {
	encoded, err := encodeEntity(content)
	if err != nil {
		return "", err
	}
	return c.create(ctx, cardData{Type: "card_json", Data: encoded})
}

func (c *Client) CreateTemplate(ctx context.Context, template TemplateData) (string, error) {
	if strings.TrimSpace(template.ID) == "" {
		return "", errors.New("feishu/card: template ID is required")
	}
	encoded, err := encodeContent(template)
	if err != nil {
		return "", err
	}
	return c.create(ctx, cardData{Type: "template", Data: encoded})
}
func (c *Client) create(ctx context.Context, input cardData) (string, error) {
	var result struct {
		CardID string `json:"card_id"`
	}
	err := c.api(ctx, "POST", "/cardkit/v1/cards", input, &result)
	if err == nil && result.CardID == "" {
		err = ErrInvalidResponse
	}
	return result.CardID, err
}

// UpdateOptions is shared across ALL operations on one Cardkit entity.
// Callers must serialize updates and strictly increase Sequence across calls,
// including settings/text/full-card updates. UUID optionally deduplicates calls.
type UpdateOptions struct {
	Sequence int32  `json:"sequence"`
	UUID     string `json:"uuid,omitempty"`
}

func (c *Client) Update(ctx context.Context, cardID string, content any, opts UpdateOptions) error {
	if err := validateUpdate(cardID, opts); err != nil {
		return err
	}
	encoded, err := encodeEntity(content)
	if err != nil {
		return err
	}
	input := struct {
		Card cardData `json:"card"`
		UpdateOptions
	}{cardData{"card_json", encoded}, opts}
	return c.api(ctx, "PUT", "/cardkit/v1/cards/"+cardID, input, nil)
}

// UpdateText replaces the FULL Markdown text, not a delta. Requires streaming_mode.
// Accumulate and throttle LLM chunks at the call site before calling this method.
func (c *Client) UpdateText(ctx context.Context, cardID, elementID, content string, opts UpdateOptions) error {
	if err := validateUpdate(cardID, opts); err != nil {
		return err
	}
	if !elementIDPattern.MatchString(elementID) || content == "" || !utf8.ValidString(content) || utf8.RuneCountInString(content) > 100000 {
		return errors.New("feishu/card: invalid element ID or text length")
	}
	input := struct {
		Content string `json:"content"`
		UpdateOptions
	}{content, opts}
	return c.api(ctx, "PUT", "/cardkit/v1/cards/"+cardID+"/elements/"+elementID+"/content", input, nil)
}

// Settings accepts a JSON object containing config and/or card_link.
func (c *Client) Settings(ctx context.Context, cardID string, settings any, opts UpdateOptions) error {
	if err := validateUpdate(cardID, opts); err != nil {
		return err
	}
	encoded, err := encodeContent(settings)
	if err != nil {
		return err
	}
	input := struct {
		Settings string `json:"settings"`
		UpdateOptions
	}{encoded, opts}
	return c.api(ctx, "PATCH", "/cardkit/v1/cards/"+cardID+"/settings", input, nil)
}

func (c *Client) Finish(ctx context.Context, cardID, summary string, opts UpdateOptions) error {
	return c.Settings(ctx, cardID, map[string]any{"config": map[string]any{"streaming_mode": false, "summary": Summary{Content: summary}}}, opts)
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var elementIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,19}$`)

func validID(id string) bool { return idPattern.MatchString(id) }
func validateUpdate(cardID string, opts UpdateOptions) error {
	if !validID(cardID) || len(cardID) > 20 || opts.Sequence <= 0 || utf8.RuneCountInString(opts.UUID) > 64 {
		return errors.New("feishu/card: invalid card ID, sequence or UUID")
	}
	return nil
}

// Bound outgoing objects conservatively to the message-card limit. Server-side
// expansion of templates/styles can still exceed the platform's size limit.
func encodeContent(content any) (string, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || len(object) == 0 {
		return "", errors.New("feishu/card: content must be a non-empty JSON object")
	}
	if len(data) > 30*1024 {
		return "", errors.New("feishu/card: content exceeds 30 KiB")
	}
	return string(data), nil
}
func encodeEntity(content any) (string, error) {
	encoded, err := encodeContent(content)
	if err != nil {
		return "", err
	}
	var fields struct {
		Schema string `json:"schema"`
		Config struct {
			UpdateMulti *bool `json:"update_multi"`
		} `json:"config"`
	}
	if json.Unmarshal([]byte(encoded), &fields) != nil || fields.Schema != "2.0" || (fields.Config.UpdateMulti != nil && !*fields.Config.UpdateMulti) {
		return "", errors.New("feishu/card: entity requires schema 2.0 and update_multi must not be false")
	}
	return encoded, nil
}
