package events

import (
	"errors"

	"google.golang.org/protobuf/encoding/protowire"
)

// Feishu's binary WebSocket envelope (pbbp2). Only this wire format is shared
// with the official SDK; no SDK service models or generated code are imported.
type header struct{ key, value string }
type frame struct {
	seq, log, service, method     uint64
	headers                       []header
	encoding, payloadType, logNew string
	payload                       []byte
}

var errProtocol = errors.New("feishu/events: invalid websocket protocol message")

func (f frame) get(key string) string {
	for _, h := range f.headers {
		if h.key == key {
			return h.value
		}
	}
	return ""
}
func (f *frame) set(key, value string) {
	for i := range f.headers {
		if f.headers[i].key == key {
			f.headers[i].value = value
			return
		}
	}
	f.headers = append(f.headers, header{key, value})
}
func appendBytes(b []byte, n protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, n, protowire.BytesType), v)
}
func (f frame) marshal() []byte {
	var b []byte
	for i, v := range []uint64{f.seq, f.log, f.service, f.method} {
		b = protowire.AppendVarint(protowire.AppendTag(b, protowire.Number(i+1), protowire.VarintType), v)
	}
	for _, h := range f.headers {
		entry := appendBytes(nil, 1, []byte(h.key))
		entry = appendBytes(entry, 2, []byte(h.value))
		b = appendBytes(b, 5, entry)
	}
	if f.encoding != "" {
		b = appendBytes(b, 6, []byte(f.encoding))
	}
	if f.payloadType != "" {
		b = appendBytes(b, 7, []byte(f.payloadType))
	}
	if f.payload != nil {
		b = appendBytes(b, 8, f.payload)
	}
	if f.logNew != "" {
		b = appendBytes(b, 9, []byte(f.logNew))
	}
	return b
}

func decodeFrame(b []byte) (frame, error) {
	var f frame
	seen := uint8(0)
	for len(b) > 0 {
		n, typ, k := protowire.ConsumeTag(b)
		if k < 0 {
			return f, errProtocol
		}
		b = b[k:]
		if n >= 1 && n <= 4 {
			if typ != protowire.VarintType {
				return f, errProtocol
			}
			v, k := protowire.ConsumeVarint(b)
			if k < 0 {
				return f, errProtocol
			}
			b = b[k:]
			seen |= 1 << uint(n-1)
			switch n {
			case 1:
				f.seq = v
			case 2:
				f.log = v
			case 3:
				f.service = v
			case 4:
				f.method = v
			}
		} else if n >= 5 && n <= 9 {
			if typ != protowire.BytesType {
				return f, errProtocol
			}
			v, k := protowire.ConsumeBytes(b)
			if k < 0 {
				return f, errProtocol
			}
			b = b[k:]
			switch n {
			case 5:
				if len(f.headers) >= 128 {
					return f, errProtocol
				}
				h, err := decodeHeader(v)
				if err != nil {
					return f, err
				}
				f.headers = append(f.headers, h)
			case 6:
				f.encoding = string(v)
			case 7:
				f.payloadType = string(v)
			case 8:
				f.payload = v
			case 9:
				f.logNew = string(v)
			}
		} else {
			k := protowire.ConsumeFieldValue(n, typ, b)
			if k < 0 {
				return f, errProtocol
			}
			b = b[k:]
		}
	}
	if seen != 15 || f.service > 1<<31-1 || f.method > 1 {
		return f, errProtocol
	}
	return f, nil
}
func decodeHeader(b []byte) (header, error) {
	var h header
	for len(b) > 0 {
		n, typ, k := protowire.ConsumeTag(b)
		if k < 0 {
			return h, errProtocol
		}
		b = b[k:]
		if n == 1 || n == 2 {
			if typ != protowire.BytesType {
				return h, errProtocol
			}
			v, k := protowire.ConsumeBytes(b)
			if k < 0 {
				return h, errProtocol
			}
			b = b[k:]
			if n == 1 {
				h.key = string(v)
			} else {
				h.value = string(v)
			}
		} else {
			k := protowire.ConsumeFieldValue(n, typ, b)
			if k < 0 {
				return h, errProtocol
			}
			b = b[k:]
		}
	}
	if h.key == "" {
		return h, errProtocol
	}
	return h, nil
}
