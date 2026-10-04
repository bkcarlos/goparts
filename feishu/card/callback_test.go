package card

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const callbackJSON = `{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","token":"verify","app_id":"app"},"event":{"operator":{"open_id":"ou_1"},"action":{"tag":"button","value":{"job_id":"42"},"form_value":{"choice":"yes"}},"context":{"open_message_id":"om_1","open_chat_id":"oc_1"}}}`

func callbackDecoder(t *testing.T, key string) *CallbackDecoder {
	t.Helper()
	d, err := NewCallbackDecoder(CallbackConfig{VerificationToken: "verify", AppID: "app", EncryptKey: key})
	if err != nil {
		t.Fatal(err)
	}
	d.now = func() time.Time { return time.Unix(1700000000, 0) }
	return d
}

func encryptedBody(plain []byte, key string) []byte {
	// Independent sender fixture: PKCS#7 + AES-CBC; IV precedes ciphertext.
	hash := sha256.Sum256([]byte(key))
	block, _ := aes.NewCipher(hash[:])
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	result := make([]byte, aes.BlockSize+len(plain))
	copy(result, []byte("0123456789abcdef"))
	cipher.NewCBCEncrypter(block, result[:aes.BlockSize]).CryptBlocks(result[aes.BlockSize:], plain)
	body, _ := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(result)})
	return body
}

func signedRequest(body []byte, key, timestamp string) *http.Request {
	r := httptest.NewRequest("POST", "/callback", bytes.NewReader(body))
	r.Header.Set("X-Lark-Request-Timestamp", timestamp)
	r.Header.Set("X-Lark-Request-Nonce", "nonce")
	r.Header.Set("X-Lark-Signature", fmt.Sprintf("%x", sha256.Sum256(append([]byte(timestamp+"nonce"+key), body...))))
	return r
}

func TestCallbackEncryptedAndPlain(t *testing.T) {
	for _, key := range []string{"", "encrypt-key"} {
		t.Run(key, func(t *testing.T) {
			d := callbackDecoder(t, key)
			body := []byte(callbackJSON)
			if key != "" {
				body = encryptedBody(body, key)
			}
			event, err := d.Decode(signedRequest(body, key, "1700000000"))
			if err != nil {
				t.Fatal(err)
			}
			if event.Header.EventID != "ev_1" || event.Event.Context.MessageID != "om_1" || event.Event.Operator.OpenID != "ou_1" {
				t.Fatalf("event=%+v", event)
			}
			var value map[string]string
			if err = json.Unmarshal(event.Event.Action.Value, &value); err != nil || value["job_id"] != "42" {
				t.Fatalf("value=%v %v", value, err)
			}
		})
	}
}

func TestChallenge(t *testing.T) {
	for _, key := range []string{"", "encrypt-key"} {
		body := []byte(`{"type":"url_verification","token":"verify","challenge":"a\"b"}`)
		if key != "" {
			body = encryptedBody(body, key)
		}
		event, err := callbackDecoder(t, key).Decode(httptest.NewRequest("POST", "/callback", bytes.NewReader(body)))
		if err != nil || event.Challenge != `a"b` {
			t.Fatalf("challenge: %+v %v", event, err)
		}
	}
}

func TestCallbackRejectsUntrustedRequests(t *testing.T) {
	key := "encrypt-key"
	for _, tc := range []struct {
		name      string
		body      []byte
		timestamp string
		tamper    bool
	}{
		{"missing_signature", encryptedBody([]byte(callbackJSON), key), "", false},
		{"expired", encryptedBody([]byte(callbackJSON), key), "1699999600", false},
		{"future", encryptedBody([]byte(callbackJSON), key), "1700000400", false},
		{"tampered", encryptedBody([]byte(callbackJSON), key), "1700000000", true},
		{"wrong_token", encryptedBody([]byte(strings.ReplaceAll(callbackJSON, "verify", "wrong")), key), "1700000000", false},
		{"wrong_app", encryptedBody([]byte(strings.ReplaceAll(callbackJSON, `"app"`, `"other"`)), key), "1700000000", false},
		{"wrong_type", encryptedBody([]byte(strings.ReplaceAll(callbackJSON, "card.action.trigger", "im.message.receive_v1")), key), "1700000000", false},
		{"missing_event", encryptedBody([]byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","token":"verify","app_id":"app"}}`), key), "1700000000", false},
		{"plaintext_downgrade", []byte(callbackJSON), "1700000000", false},
		{"bad_base64", []byte(`{"encrypt":"%%%"}`), "1700000000", false},
		{"short_cipher", []byte(`{"encrypt":"YWJjZA=="}`), "1700000000", false},
		{"malformed", []byte(`{`), "1700000000", false},
		{"wrong_challenge_token", encryptedBody([]byte(`{"type":"url_verification","challenge":"x","token":"wrong"}`), key), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := signedRequest(tc.body, key, tc.timestamp)
			if tc.tamper {
				r.Header.Set("X-Lark-Signature", strings.Repeat("0", 64))
			}
			if _, err := callbackDecoder(t, key).Decode(r); !errors.Is(err, ErrInvalidCallback) {
				t.Fatalf("accepted callback: %v", err)
			}
		})
	}
	d := callbackDecoder(t, "")
	for _, r := range []*http.Request{nil, httptest.NewRequest("GET", "/callback", nil), httptest.NewRequest("POST", "/callback", strings.NewReader(strings.Repeat("x", callbackLimit+1))), httptest.NewRequest("POST", "/callback", bytes.NewReader(encryptedBody([]byte(callbackJSON), key)))} {
		if _, err := d.Decode(r); !errors.Is(err, ErrInvalidCallback) {
			t.Fatalf("accepted invalid request: %v", err)
		}
	}
}

func TestInvalidPaddingAndResponse(t *testing.T) {
	body := encryptedBody([]byte(callbackJSON), "key")
	var wrapper map[string]string
	_ = json.Unmarshal(body, &wrapper)
	ciphertext, _ := base64.StdEncoding.DecodeString(wrapper["encrypt"])
	// Change the final plaintext padding byte through the previous CBC block.
	ciphertext[len(ciphertext)-aes.BlockSize-1] ^= 0xff
	if _, err := decryptCallback(base64.StdEncoding.EncodeToString(ciphertext), "key"); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("bad padding: %v", err)
	}
	response := CallbackResponse{Toast: &Toast{Type: "success", Content: "已确认"}, Card: RawResponseCard(NewCard("已完成"))}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"type":"raw"`) || !strings.Contains(string(data), `"toast":{"type":"success"`) {
		t.Fatalf("response=%s", data)
	}
	response.Card = TemplateResponseCard(TemplateData{ID: "tpl"})
	data, err = json.Marshal(response)
	if err != nil || !strings.Contains(string(data), `"template_id":"tpl"`) {
		t.Fatalf("template response=%s %v", data, err)
	}
}

func TestDecoderConfiguration(t *testing.T) {
	for _, cfg := range []CallbackConfig{{}, {VerificationToken: "v"}, {AppID: "app"}, {VerificationToken: "v", AppID: "app", MaxAge: -time.Second}} {
		if _, err := NewCallbackDecoder(cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
