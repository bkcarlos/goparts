package legacy_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bkcarlos/goparts/feishu/card/legacy"
)

func TestLegacyCardWireSchema(t *testing.T) {
	c := legacy.New("标题", legacy.Div("**正文**"), legacy.Note("备注"), legacy.HR(), legacy.Action(legacy.Button("确认", map[string]any{"action": "confirm"}), legacy.Link("打开", "https://example.com")))
	raw, err := c.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	expected := `{"config":{"wide_screen_mode":true,"enable_forward":true,"update_multi":false},"header":{"title":{"tag":"plain_text","content":"标题"}},"elements":[{"tag":"div","text":{"tag":"lark_md","content":"**正文**"}},{"tag":"note","elements":[{"tag":"plain_text","content":"备注"}]},{"tag":"hr"},{"tag":"action","actions":[{"tag":"button","text":{"tag":"plain_text","content":"确认"},"type":"default","value":{"action":"confirm"}},{"tag":"button","text":{"tag":"plain_text","content":"打开"},"type":"default","url":"https://example.com"}]}]}`
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrong legacy schema: %s", raw)
	}
}

func TestInvalidCardCannotBeSerialized(t *testing.T) {
	for _, c := range []legacy.Card{legacy.New("empty"), legacy.New("bad value", legacy.Button("button", map[string]any{"unsupported": make(chan int)}))} {
		if _, err := c.JSON(); err == nil {
			t.Fatal("invalid card serialized")
		}
	}
	raw, err := legacy.New("", legacy.HR()).JSON()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["header"]; exists {
		t.Fatal("empty header included")
	}
	if _, exists := got["schema"]; exists {
		t.Fatal("legacy card silently converted to schema 2")
	}
}
