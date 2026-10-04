// Package card builds Feishu JSON 2.0 cards and provides application-bot APIs.
// It has no dependency on the webhook or user-login packages.
package card

import "encoding/json"

type Text struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

func PlainText(content string) Text { return Text{Tag: "plain_text", Content: content} }

type Header struct {
	Title    Text   `json:"title"`
	Subtitle *Text  `json:"subtitle,omitempty"`
	Template string `json:"template,omitempty"`
}

type CardConfig struct {
	UpdateMulti   bool     `json:"update_multi"`
	StreamingMode bool     `json:"streaming_mode,omitempty"`
	Summary       *Summary `json:"summary,omitempty"`
}

type Summary struct {
	Content string `json:"content"`
}
type Body struct {
	Elements []any `json:"elements"`
}

// Card is mutable while building. Do not mutate it during serialization/sending.
type Card struct {
	Schema string     `json:"schema"`
	Config CardConfig `json:"config"`
	Header *Header    `json:"header,omitempty"`
	Body   Body       `json:"body"`
}

func NewCard(title string) *Card {
	c := &Card{Schema: "2.0", Config: CardConfig{UpdateMulti: true}, Body: Body{Elements: []any{}}}
	if title != "" {
		c.Header = &Header{Title: PlainText(title), Template: "blue"}
	}
	return c
}

func (c *Card) Add(elements ...any) *Card {
	c.Body.Elements = append(c.Body.Elements, elements...)
	return c
}
func (c *Card) WithSummary(summary string) *Card {
	c.Config.Summary = &Summary{Content: summary}
	return c
}
func (c *Card) WithStreaming(enabled bool) *Card { c.Config.StreamingMode = enabled; return c }
func (c *Card) JSON() ([]byte, error)            { return json.Marshal(c) }

// Element is open for less common components and future protocol fields.
type Element map[string]any

func Markdown(content string) Element { return Element{"tag": "markdown", "content": content} }
func Divider() Element                { return Element{"tag": "hr"} }
func Image(imageKey, alt string) Element {
	return Element{"tag": "img", "img_key": imageKey, "alt": PlainText(alt)}
}
func (e Element) WithID(id string) Element { e["element_id"] = id; return e }

func LinkButton(label, targetURL string) Element {
	return button(label, map[string]any{"type": "open_url", "default_url": targetURL})
}
func CallbackButton(label string, value map[string]any) Element {
	return button(label, map[string]any{"type": "callback", "value": value})
}
func button(label string, behavior map[string]any) Element {
	return Element{"tag": "button", "text": PlainText(label), "type": "default", "behaviors": []any{behavior}}
}

type TemplateData struct {
	ID        string         `json:"template_id"`
	Version   string         `json:"template_version_name,omitempty"`
	Variables map[string]any `json:"template_variable,omitempty"`
}

// Template returns message content (also accepted by webhook feishu.Card).
func Template(id, version string, variables map[string]any) Element {
	return Element{"type": "template", "data": TemplateData{ID: id, Version: version, Variables: variables}}
}

// Reference sends a previously created Cardkit entity; each entity can be sent once.
func Reference(cardID string) Element {
	return Element{"type": "card", "data": map[string]string{"card_id": cardID}}
}
