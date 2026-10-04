// Package events receives Feishu events through an authenticated WebSocket.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/bkcarlos/goparts/feishu/card"
)

const MessageReceive = "im.message.receive_v1"
const CardAction = "card.action.trigger"
const ChatMemberBotAdded = "im.chat.member.bot.added_v1"

type Header struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
	CreateTime string `json:"create_time"`
}
type Event struct {
	Schema string          `json:"schema"`
	Header Header          `json:"header"`
	Data   json.RawMessage `json:"event"`
}
type UserID struct {
	OpenID  string `json:"open_id"`
	UserID  string `json:"user_id"`
	UnionID string `json:"union_id"`
}
type Message struct {
	MessageID   string            `json:"message_id"`
	RootID      string            `json:"root_id"`
	ParentID    string            `json:"parent_id"`
	CreateTime  string            `json:"create_time"`
	UpdateTime  string            `json:"update_time"`
	ChatID      string            `json:"chat_id"`
	ChatType    string            `json:"chat_type"`
	MessageType string            `json:"message_type"`
	Content     string            `json:"content"` // JSON encoded string, interpreted by message_type
	Mentions    []json.RawMessage `json:"mentions"`
}
type MessageEvent struct {
	Header Header `json:"-"`
	Sender struct {
		ID        UserID `json:"sender_id"`
		Type      string `json:"sender_type"`
		TenantKey string `json:"tenant_key"`
	} `json:"sender"`
	Message Message `json:"message"`
}
type BotAddedEvent struct {
	Header     Header `json:"-"`
	ChatID     string `json:"chat_id"`
	OperatorID UserID `json:"operator_id"`
	External   bool   `json:"external"`
	Name       string `json:"name"`
}

// Handler returns a JSON-serializable callback result (usually nil). A failure
// receives a non-success acknowledgement so Feishu can redeliver the event.
type Handler func(context.Context, Event) (any, error)
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewDispatcher() *Dispatcher { return &Dispatcher{handlers: make(map[string]Handler)} }
func (d *Dispatcher) On(eventType string, handler Handler) error {
	if eventType == "" || handler == nil {
		return errors.New("feishu/events: event type and handler are required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handlers == nil {
		d.handlers = make(map[string]Handler)
	}
	if _, ok := d.handlers[eventType]; ok {
		return errors.New("feishu/events: handler already registered")
	}
	d.handlers[eventType] = handler
	return nil
}
func (d *Dispatcher) OnP2MessageReceive(fn func(context.Context, MessageEvent) error) error {
	if fn == nil {
		return errors.New("feishu/events: handler is required")
	}
	return d.On(MessageReceive, func(ctx context.Context, e Event) (any, error) {
		var data MessageEvent
		if json.Unmarshal(e.Data, &data) != nil || data.Message.MessageID == "" {
			return nil, errProtocol
		}
		data.Header = e.Header
		return nil, fn(ctx, data)
	})
}
func (d *Dispatcher) OnCardAction(fn func(context.Context, card.CallbackRequest) (*card.CallbackResponse, error)) error {
	if fn == nil {
		return errors.New("feishu/events: handler is required")
	}
	return d.On(CardAction, func(ctx context.Context, e Event) (any, error) {
		var data card.CallbackEvent
		if json.Unmarshal(e.Data, &data) != nil || data.Action.Tag == "" {
			return nil, errProtocol
		}
		response, err := fn(ctx, card.CallbackRequest{Schema: e.Schema, Header: card.CallbackHeader{EventID: e.Header.EventID, EventType: e.Header.EventType, AppID: e.Header.AppID, TenantKey: e.Header.TenantKey, CreateTime: e.Header.CreateTime}, Event: &data})
		if response == nil {
			return nil, err
		}
		return response, err
	})
}
func (d *Dispatcher) OnChatMemberBotAdded(fn func(context.Context, BotAddedEvent) error) error {
	if fn == nil {
		return errors.New("feishu/events: handler is required")
	}
	return d.On(ChatMemberBotAdded, func(ctx context.Context, e Event) (any, error) {
		var data BotAddedEvent
		if json.Unmarshal(e.Data, &data) != nil || data.ChatID == "" {
			return nil, errProtocol
		}
		data.Header = e.Header
		return nil, fn(ctx, data)
	})
}
func (d *Dispatcher) dispatch(ctx context.Context, e Event) (response any, err error) {
	defer func() {
		if recover() != nil {
			response = nil
			err = errors.New("feishu/events: handler panicked")
		}
	}()
	d.mu.RLock()
	handler := d.handlers[e.Header.EventType]
	d.mu.RUnlock()
	if handler == nil {
		return nil, nil
	} // unregistered events are intentionally acknowledged
	return handler(ctx, e)
}
