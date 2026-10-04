package feishu

// Message is a custom bot message. Use the constructors for common message types.
// Content and Card may also contain provider-supported custom JSON structures.
// Do not mutate maps or slices while a message is being sent.
type Message struct {
	MsgType string `json:"msg_type"`
	Content any    `json:"content,omitempty"`
	Card    any    `json:"card,omitempty"`
}

func Text(text string) Message {
	return Message{MsgType: "text", Content: map[string]string{"text": text}}
}

// Image sends an existing image_key. Uploading images requires an application API.
func Image(imageKey string) Message {
	return Message{MsgType: "image", Content: map[string]string{"image_key": imageKey}}
}

// PostElement represents common rich-text tags: text, a, at, and img.
type PostElement struct {
	Tag      string `json:"tag"`
	Text     string `json:"text,omitempty"`
	Href     string `json:"href,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	ImageKey string `json:"image_key,omitempty"`
}

type PostContent struct {
	Title   string          `json:"title"`
	Content [][]PostElement `json:"content"`
}

// Post accepts localized content keyed by locale, for example "zh_cn".
func Post(locales map[string]PostContent) Message {
	return Message{MsgType: "post", Content: map[string]any{"post": locales}}
}

// Card accepts a Feishu card JSON object. Webhook cards do not implement event callbacks.
func Card(card any) Message {
	return Message{MsgType: "interactive", Card: card}
}

// Markdown builds a basic card containing a title and a lark_md body.
func Markdown(title, content string) Message {
	return Card(map[string]any{
		"config": map[string]bool{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": title}},
		"elements": []any{map[string]any{
			"tag": "div", "text": map[string]string{"tag": "lark_md", "content": content},
		}},
	})
}
