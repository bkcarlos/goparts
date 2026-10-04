package events

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

func eventFrame(id, kind string, data string) frame {
	return frame{seq: 7, log: 9, service: 1, method: 1, headers: []header{{"type", "event"}, {"message_id", id}, {"sum", "1"}, {"seq", "0"}, {"trace_id", "trace1"}}, payload: []byte(`{"schema":"2.0","header":{"event_id":"` + id + `","event_type":"` + kind + `","app_id":"app"},"event":` + data + `}`)}
}

func TestWireGoldenAndInvalidFrames(t *testing.T) {
	// Independent fixed protobuf fixture: fields 1..4, header type=event,
	// payload={}. Integers and header values must survive ACK serialization.
	golden, _ := hex.DecodeString("08071009180120012a0d0a047479706512056576656e7442027b7d")
	f, err := decodeFrame(golden)
	if err != nil || f.seq != 7 || f.log != 9 || f.service != 1 || f.method != 1 || f.get("type") != "event" || string(f.payload) != "{}" {
		t.Fatalf("frame=%+v err=%v", f, err)
	}
	if !bytes.Equal(f.marshal(), golden) {
		t.Fatalf("wire mismatch %x", f.marshal())
	}
	for _, raw := range [][]byte{nil, {0x08}, {0x0a, 0x00}, {0x08, 0x01, 0x10, 0x00, 0x18, 0x00, 0x20, 0x02}, append(bytes.Clone(golden), 0x2a, 0xff)} {
		if _, err := decodeFrame(raw); err == nil {
			t.Errorf("accepted %x", raw)
		}
	}
}

func TestFragmentsLimitsOrderAndExpiry(t *testing.T) {
	a := assembler{limit: 8, groups: map[string]*fragments{}}
	now := time.Now()
	f := eventFrame("one", MessageReceive, `{}`)
	f.set("sum", "2")
	f.set("seq", "1")
	f.payload = []byte("def")
	if _, ready, err := a.add(f, now); err != nil || ready {
		t.Fatalf("first: ready=%v err=%v", ready, err)
	}
	if _, ready, err := a.add(f, now); err != nil || ready {
		t.Fatalf("duplicate: ready=%v err=%v", ready, err)
	}
	f.set("seq", "0")
	f.payload = []byte("abc")
	joined, ready, err := a.add(f, now)
	if err != nil || !ready || string(joined.payload) != "abcdef" || a.bytes != 0 {
		t.Fatalf("joined=%+v ready=%v err=%v", joined, ready, err)
	}
	for _, sum := range []string{"0", "-1", "129", "oops"} {
		f.set("sum", sum)
		if _, _, err := a.add(f, now); err == nil {
			t.Error("invalid count accepted")
		}
	}
	f.set("sum", "2")
	f.payload = []byte("12345678")
	f.set("seq", "0")
	if _, _, err := a.add(f, now); err != nil {
		t.Fatal(err)
	}
	f.set("seq", "1")
	if _, _, err := a.add(f, now); err == nil {
		t.Fatal("oversize assembled event accepted")
	}
	f.payload = []byte("x")
	if _, ready, err := a.add(f, now.Add(6*time.Second)); err != nil || ready || a.bytes != 1 {
		t.Fatal("expired fragments retained")
	}
	// Different copies with the same sequence must never be silently combined.
	f.payload = []byte("y")
	if _, _, err := a.add(f, now.Add(6*time.Second)); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("private data") }

func TestHandlerACKFailuresAndTimeout(t *testing.T) {
	for name, handler := range map[string]Handler{
		"error":           func(context.Context, Event) (any, error) { return nil, errors.New("failure") },
		"panic":           func(context.Context, Event) (any, error) { panic("private data") },
		"marshal panic":   func(context.Context, Event) (any, error) { return panicJSON{}, nil },
		"marshal failure": func(context.Context, Event) (any, error) { return make(chan int), nil },
		"too large":       func(context.Context, Event) (any, error) { return string(make([]byte, 1024)), nil },
		"deadline":        func(ctx context.Context, _ Event) (any, error) { <-ctx.Done(); return nil, nil },
	} {
		t.Run(name, func(t *testing.T) {
			d := NewDispatcher()
			if err := d.On("custom", handler); err != nil {
				t.Fatal(err)
			}
			c, err := New(Config{AppID: "app", AppSecret: "secret", Dispatcher: d, HandlerTimeout: time.Millisecond, MaxResponseBytes: 100})
			if err != nil {
				t.Fatal(err)
			}
			ack := c.handle(context.Background(), eventFrame("id", "custom", `{}`))
			var body struct {
				Code int    `json:"code"`
				Data []byte `json:"data"`
			}
			if json.Unmarshal(ack.payload, &body) != nil || body.Code != 500 || len(body.Data) > 0 || ack.get("biz_rt") == "" {
				t.Fatalf("ack=%s", ack.payload)
			}
		})
	}
	d := NewDispatcher()
	calls := 0
	if err := d.On("custom", func(context.Context, Event) (any, error) { calls++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := d.On("custom", func(context.Context, Event) (any, error) { return nil, nil }); err == nil {
		t.Fatal("duplicate registration")
	}
	c, _ := New(Config{AppID: "app", AppSecret: "secret", Dispatcher: d})
	f := eventFrame("id", "custom", `{}`)
	f.payload = bytes.ReplaceAll(f.payload, []byte(`"app"`), []byte(`"other-app"`))
	c.handle(context.Background(), f)
	if calls != 0 {
		t.Fatal("dispatched event for another app")
	}
}

func FuzzFrameDecode(f *testing.F) {
	f.Add(eventFrame("id", MessageReceive, `{}`).marshal())
	f.Add([]byte{0x08, 0x80})
	f.Fuzz(func(t *testing.T, b []byte) {
		frame, err := decodeFrame(b)
		if err != nil {
			return
		}
		encoded := frame.marshal()
		round, err := decodeFrame(encoded)
		if err != nil || round.seq != frame.seq || !bytes.Equal(round.payload, frame.payload) {
			t.Fatal("invalid round trip", strconv.Itoa(len(b)))
		}
	})
}
