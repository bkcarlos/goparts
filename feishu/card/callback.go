package card

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/bkcarlos/goparts/feishu/dedup"
	"io"
	"net/http"
	"strconv"
	"time"
)

var ErrInvalidCallback = errors.New("feishu/card: invalid or unauthenticated callback")

const callbackLimit = 1024 * 1024

type CallbackConfig struct {
	Deduper           dedup.Store
	VerificationToken string
	AppID             string
	EncryptKey        string        // optional; configured callbacks require encrypted payloads and signed events
	MaxAge            time.Duration // signature timestamp tolerance, default 5 minutes
}

// CallbackDecoder verifies HTTP card.action.trigger callbacks (schema 2.0).
// It does not authorize business actions or deduplicate event IDs.
type CallbackDecoder struct {
	config CallbackConfig
	now    func() time.Time
}

func NewCallbackDecoder(cfg CallbackConfig) (*CallbackDecoder, error) {
	if cfg.VerificationToken == "" || cfg.AppID == "" || cfg.MaxAge < 0 {
		return nil, errors.New("feishu/card: callback requires VerificationToken, AppID and nonnegative MaxAge")
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 5 * time.Minute
	}
	return &CallbackDecoder{config: cfg, now: time.Now}, nil
}

type CallbackHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
	CreateTime string `json:"create_time"`
	Token      string `json:"token"`
}
type Operator struct {
	OpenID    string `json:"open_id"`
	UserID    string `json:"user_id"`
	UnionID   string `json:"union_id"`
	TenantKey string `json:"tenant_key"`
}
type Action struct {
	Tag        string                     `json:"tag"`
	Name       string                     `json:"name"`
	Value      json.RawMessage            `json:"value"`
	FormValue  map[string]json.RawMessage `json:"form_value"`
	Option     string                     `json:"option"`
	Options    []string                   `json:"options"`
	InputValue string                     `json:"input_value"`
	Timezone   string                     `json:"timezone"`
}
type CallbackContext struct {
	MessageID    string `json:"open_message_id"`
	ChatID       string `json:"open_chat_id"`
	URL          string `json:"url"`
	PreviewToken string `json:"preview_token"`
}
type CallbackEvent struct {
	Operator Operator        `json:"operator"`
	Token    string          `json:"token"`
	Action   Action          `json:"action"`
	Host     string          `json:"host"`
	Context  CallbackContext `json:"context"`
}

// Challenge is set only for an authenticated URL-verification request.
// For a normal callback, Header and Event hold the authenticated event.
type CallbackRequest struct {
	Schema    string         `json:"schema"`
	Type      string         `json:"type"`
	Token     string         `json:"token"`
	Challenge string         `json:"challenge"`
	Header    CallbackHeader `json:"header"`
	Event     *CallbackEvent `json:"event"`
}

// Decode consumes at most 1 MiB of r.Body. When EncryptKey is set, only the URL
// challenge may omit a signature (as in Feishu's SDK); its token is still checked.
// Replayed valid events inside MaxAge remain possible: deduplicate Header.EventID
// in the business layer, with a cached response for retried deliveries.
func (d *CallbackDecoder) Decode(r *http.Request) (*CallbackRequest, error) {
	if r == nil || r.Method != http.MethodPost || r.Body == nil {
		return nil, ErrInvalidCallback
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, callbackLimit+1))
	if err != nil || len(raw) > callbackLimit {
		return nil, ErrInvalidCallback
	}
	plain := raw
	var wrapper struct {
		Encrypt string `json:"encrypt"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return nil, ErrInvalidCallback
	}
	if d.config.EncryptKey != "" {
		if wrapper.Encrypt == "" {
			return nil, ErrInvalidCallback
		}
		plain, err = decryptCallback(wrapper.Encrypt, d.config.EncryptKey)
		if err != nil {
			return nil, ErrInvalidCallback
		}
	} else if wrapper.Encrypt != "" {
		return nil, ErrInvalidCallback
	}
	var result CallbackRequest
	if json.Unmarshal(plain, &result) != nil {
		return nil, ErrInvalidCallback
	}
	if result.Type == "url_verification" {
		if result.Challenge == "" || !equalSecret(result.Token, d.config.VerificationToken) {
			return nil, ErrInvalidCallback
		}
		return &result, nil
	}
	if d.config.EncryptKey != "" && !d.verifySignature(r.Header, raw) {
		return nil, ErrInvalidCallback
	}
	if result.Schema != "2.0" || result.Header.EventType != "card.action.trigger" || result.Header.AppID != d.config.AppID || result.Header.EventID == "" || result.Event == nil || !equalSecret(result.Header.Token, d.config.VerificationToken) {
		return nil, ErrInvalidCallback
	}
	// Only URL verification may be interpreted as a challenge by the caller.
	result.Challenge = ""
	return &result, nil
}

func equalSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (d *CallbackDecoder) verifySignature(h http.Header, body []byte) bool {
	timestamp, nonce := h.Get("X-Lark-Request-Timestamp"), h.Get("X-Lark-Request-Nonce")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || nonce == "" {
		return false
	}
	at, now := time.Unix(seconds, 0), d.now()
	if at.Before(now.Add(-d.config.MaxAge)) || at.After(now.Add(d.config.MaxAge)) {
		return false
	}
	hash := sha256.New()
	hash.Write([]byte(timestamp))
	hash.Write([]byte(nonce))
	hash.Write([]byte(d.config.EncryptKey))
	hash.Write(body)
	actual, err := hex.DecodeString(h.Get("X-Lark-Signature"))
	return err == nil && subtle.ConstantTimeCompare(actual, hash.Sum(nil)) == 1
}

func decryptCallback(encoded, key string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 2*aes.BlockSize || (len(data)-aes.BlockSize)%aes.BlockSize != 0 {
		return nil, ErrInvalidCallback
	}
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, ErrInvalidCallback
	}
	plain := make([]byte, len(data)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, data[:aes.BlockSize]).CryptBlocks(plain, data[aes.BlockSize:])
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || !bytes.Equal(plain[len(plain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, ErrInvalidCallback
	}
	return plain[:len(plain)-padding], nil
}

type Toast struct {
	Type    string            `json:"type"` // info, success, warning, error
	Content string            `json:"content,omitempty"`
	I18n    map[string]string `json:"i18n,omitempty"`
}
type ResponseCard struct {
	Type string `json:"type"` // raw or template
	Data any    `json:"data"`
}
type CallbackResponse struct {
	Toast *Toast        `json:"toast,omitempty"`
	Card  *ResponseCard `json:"card,omitempty"`
}

func RawResponseCard(content any) *ResponseCard { return &ResponseCard{Type: "raw", Data: content} }
func TemplateResponseCard(template TemplateData) *ResponseCard {
	return &ResponseCard{Type: "template", Data: template}
}

// Handler verifies callbacks before optional deduplication and caches only
// successful encoded responses. Failed callbacks can be retried by Feishu.
func (d *CallbackDecoder) Handler(fn func(context.Context, *CallbackRequest) (*CallbackResponse, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		event, err := d.Decode(r)
		if err != nil {
			http.Error(w, "invalid callback", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if event.Challenge != "" {
			json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
			return
		}
		run := func() ([]byte, error) {
			if fn == nil {
				return nil, errors.New("card: callback handler required")
			}
			response, err := fn(r.Context(), event)
			if err != nil {
				return nil, err
			}
			return json.Marshal(response)
		}
		var data []byte
		if d.config.Deduper != nil {
			data, err = d.config.Deduper.Do(r.Context(), d.config.AppID+":"+event.Header.EventID, run)
		} else {
			data, err = run()
		}
		if err != nil {
			http.Error(w, "callback failed", 500)
			return
		}
		w.Write(data)
	})
}
