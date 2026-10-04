// Package legacy builds Feishu card JSON 1.0 without silently converting schemas.
package legacy

import (
	"encoding/json"
	"errors"
)

type Text struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

func Plain(text string) Text    { return Text{"plain_text", text} }
func Markdown(text string) Text { return Text{"lark_md", text} }

type Element map[string]any
type Card struct {
	Config   Config    `json:"config"`
	Header   *Header   `json:"header,omitempty"`
	Elements []Element `json:"elements"`
}
type Config struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
	UpdateMulti    bool `json:"update_multi"`
}
type Header struct {
	Title    Text   `json:"title"`
	Template string `json:"template,omitempty"`
}

func New(title string, elements ...Element) Card {
	c := Card{Config: Config{WideScreenMode: true, EnableForward: true}, Elements: append([]Element(nil), elements...)}
	if title != "" {
		c.Header = &Header{Title: Plain(title)}
	}
	return c
}
func Div(text string) Element  { return Element{"tag": "div", "text": Markdown(text)} }
func Note(text string) Element { return Element{"tag": "note", "elements": []Text{Plain(text)}} }
func HR() Element              { return Element{"tag": "hr"} }
func Button(label string, value map[string]any) Element {
	return Element{"tag": "button", "text": Plain(label), "type": "default", "value": value}
}
func Link(label, url string) Element {
	return Element{"tag": "button", "text": Plain(label), "url": url, "type": "default"}
}
func Action(buttons ...Element) Element { return Element{"tag": "action", "actions": buttons} }
func (c Card) JSON() (json.RawMessage, error) {
	if len(c.Elements) == 0 {
		return nil, errors.New("legacy: card needs elements")
	}
	return json.Marshal(c)
}
