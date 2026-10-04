package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Client) pingInterval(conf serverConfig) time.Duration {
	if c.cfg.PingInterval > 0 {
		return c.cfg.PingInterval
	}
	if conf.PingInterval != nil && *conf.PingInterval > 0 {
		return time.Duration(*conf.PingInterval) * time.Second
	}
	return 120 * time.Second
}

func (c *Client) session(parent context.Context, conn *websocket.Conn, service uint64, conf serverConfig) (last serverConfig, result error) {
	ctx, cancel := context.WithCancel(parent)
	var current atomic.Pointer[serverConfig]
	current.Store(&conf)
	var wg sync.WaitGroup
	var writeMu sync.Mutex
	write := func(f frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout)); err != nil {
			return &TransportError{Operation: "write deadline", cause: err}
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, f.marshal()); err != nil {
			return &TransportError{Operation: "websocket write", cause: err}
		}
		return nil
	}
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
		conn.Close()
	}
	changes := make(chan struct{}, 1)
	jobs := make(chan frame, c.cfg.QueueSize)
	conn.SetReadLimit(c.cfg.MaxFrameBytes)
	defer func() { cancel(); conn.Close(); wg.Wait(); last = *current.Load() }()
	wg.Add(3)
	go func() { defer wg.Done(); <-ctx.Done(); conn.Close() }()
	go func() {
		defer wg.Done()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(c.pingInterval(*current.Load()))
			case <-timer.C:
				if err := write(frame{service: service, headers: []header{{"type", "ping"}}}); err != nil {
					fail(err)
					return
				}
				timer.Reset(c.pingInterval(*current.Load()))
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-jobs:
				if ctx.Err() != nil {
					return
				}
				ack := c.handle(ctx, f)
				if err := write(ack); err != nil {
					fail(err)
					return
				}
			}
		}
	}()
	if c.cfg.OnConnected != nil {
		func() {
			defer func() {
				if recover() != nil {
					c.report(ctx, errors.New("feishu/events: OnConnected panicked"))
				}
			}()
			c.cfg.OnConnected(ctx)
		}()
	}
	parts := assembler{limit: c.cfg.MaxEventBytes, groups: make(map[string]*fragments)}
	for {
		if err := conn.SetReadDeadline(time.Now().Add(2*c.pingInterval(*current.Load()) + 5*time.Second)); err != nil {
			return conf, &TransportError{Operation: "read deadline", cause: err}
		}
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case err = <-failures:
			default:
				err = &TransportError{Operation: "websocket read", cause: err}
			}
			return conf, err
		}
		if kind != websocket.BinaryMessage {
			return conf, errProtocol
		}
		f, err := decodeFrame(raw)
		if err != nil {
			return conf, err
		}
		if f.encoding != "" && f.encoding != "json" {
			return conf, errProtocol
		}
		if f.method == 0 {
			if f.get("type") == "pong" && len(f.payload) > 0 {
				var next serverConfig
				if json.Unmarshal(f.payload, &next) != nil {
					return conf, errProtocol
				}
				merged, err := mergeConfig(*current.Load(), next)
				if err != nil {
					return conf, err
				}
				current.Store(&merged)
				select {
				case changes <- struct{}{}:
				default:
				}
			}
			continue
		}
		if f.get("type") != "event" && f.get("type") != "card" {
			return conf, errProtocol
		}
		f, ready, err := parts.add(f, time.Now())
		if err != nil {
			return conf, err
		}
		if !ready {
			continue
		}
		select {
		case jobs <- f:
		case <-ctx.Done():
			return conf, ctx.Err()
		default:
			return conf, &TransportError{Operation: "event queue capacity", cause: errors.New("event queue full")}
		}
	}
}

func (c *Client) handle(ctx context.Context, f frame) frame {
	start := time.Now()
	var e Event
	var response any
	err := json.Unmarshal(f.payload, &e)
	if err != nil || e.Schema != "2.0" || e.Header.AppID != c.cfg.AppID || e.Header.EventID == "" || e.Header.EventType == "" || len(e.Data) == 0 || string(e.Data) == "null" {
		err = errProtocol
	} else {
		bounded, cancel := context.WithTimeout(ctx, c.cfg.HandlerTimeout)
		response, err = c.cfg.Dispatcher.dispatch(bounded, e)
		if err == nil {
			err = bounded.Err()
		}
		cancel()
	}
	// []byte intentionally encodes as base64, per Feishu's acknowledgement
	// protocol. A raw JSON object here would break interactive card responses.
	ack := struct {
		Code    int               `json:"code"`
		Headers map[string]string `json:"headers"`
		Data    []byte            `json:"data"`
	}{Code: 200}
	if err == nil && response != nil {
		ack.Data, err = marshalResponse(response)
		if int64(len(ack.Data)) > c.cfg.MaxResponseBytes {
			err = errors.New("feishu/events: handler response exceeds size limit")
		}
	}
	if err != nil {
		ack.Code = 500
		ack.Data = nil
		c.report(ctx, err)
	}
	f.payload, _ = json.Marshal(ack)
	f.set("biz_rt", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	return f
}

type fragments struct {
	parts   [][]byte
	bytes   int64
	count   int
	expires time.Time
	service uint64
}
type assembler struct {
	limit, bytes int64
	groups       map[string]*fragments
}

// Fragment buffers live only for a connection, expire after five seconds, and
// share a total budget of twice MaxEventBytes (at most 64 pending messages).
func (a *assembler) add(f frame, now time.Time) (frame, bool, error) {
	for key, g := range a.groups {
		if !now.Before(g.expires) {
			a.bytes -= g.bytes
			delete(a.groups, key)
		}
	}
	sum, err := strconv.Atoi(f.get("sum"))
	if err != nil || sum < 1 || sum > 128 {
		return f, false, errProtocol
	}
	seq, err := strconv.Atoi(f.get("seq"))
	if err != nil || seq < 0 || seq >= sum || f.get("message_id") == "" {
		return f, false, errProtocol
	}
	if int64(len(f.payload)) > a.limit {
		return f, false, errProtocol
	}
	if sum == 1 {
		return f, true, nil
	}
	key := f.get("message_id") + "/" + f.get("type")
	g := a.groups[key]
	if g == nil {
		if len(a.groups) >= 64 {
			return f, false, errProtocol
		}
		g = &fragments{parts: make([][]byte, sum), expires: now.Add(5 * time.Second), service: f.service}
		a.groups[key] = g
	}
	if len(g.parts) != sum || g.service != f.service || len(f.payload) == 0 {
		return f, false, errProtocol
	}
	if g.parts[seq] != nil {
		if !bytes.Equal(g.parts[seq], f.payload) {
			return f, false, errProtocol
		}
		return f, false, nil
	}
	if g.bytes+int64(len(f.payload)) > a.limit || a.bytes+int64(len(f.payload)) > 2*a.limit {
		return f, false, errProtocol
	}
	g.parts[seq] = bytes.Clone(f.payload)
	g.count++
	g.bytes += int64(len(f.payload))
	a.bytes += int64(len(f.payload))
	if g.count < sum {
		return f, false, nil
	}
	f.payload = make([]byte, 0, int(g.bytes))
	for _, p := range g.parts {
		f.payload = append(f.payload, p...)
	}
	a.bytes -= g.bytes
	delete(a.groups, key)
	return f, true, nil
}

func marshalResponse(value any) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data = nil
			err = errors.New("feishu/events: response marshaler panicked")
		}
	}()
	return json.Marshal(value)
}
