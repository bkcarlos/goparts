// Package chat provides optional application helpers on top of llm transports.
package chat

import (
	"errors"
	"github.com/bkcarlos/goparts/llm"
	"sync"
)

var ErrBudget = errors.New("chat: latest turn and pinned messages exceed budget")

type SessionConfig struct {
	MaxMessages, MaxTokens int
	CountTokens            func([]llm.Message) int
}
type Session struct {
	mu       sync.Mutex
	cfg      SessionConfig
	messages []llm.Message
}

func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.MaxMessages < 0 || cfg.MaxTokens < 0 || cfg.MaxTokens > 0 && cfg.CountTokens == nil {
		return nil, errors.New("chat: invalid session budget/token counter")
	}
	return &Session{cfg: cfg}, nil
}
func cloneMessages(in []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), in...)
	for i := range out {
		out[i].ToolCalls = append([]llm.ToolCall(nil), in[i].ToolCalls...)
	}
	return out
}
func (s *Session) Messages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMessages(s.messages)
}
func (s *Session) Reset() { s.mu.Lock(); s.messages = nil; s.mu.Unlock() }

// Append trims complete oldest turns (user through all assistant/tool messages).
// Leading system/developer messages stay pinned. A too-large latest turn returns
// ErrBudget without mutation. Tool results must match a preceding call exactly once.
func (s *Session) Append(messages ...llm.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := append(cloneMessages(s.messages), cloneMessages(messages)...)
	pending := map[string]bool{}
	for _, m := range candidate {
		if m.Role == llm.RoleTool {
			if !pending[m.ToolCallID] {
				return errors.New("chat: orphan or duplicate tool result")
			}
			delete(pending, m.ToolCallID)
		} else {
			if len(pending) > 0 {
				return errors.New("chat: unresolved tool calls before next message")
			}
			for _, call := range m.ToolCalls {
				if call.ID == "" || pending[call.ID] {
					return errors.New("chat: invalid tool call ID")
				}
				pending[call.ID] = true
			}
		}
	}
	pinned := 0
	for pinned < len(candidate) && (candidate[pinned].Role == llm.RoleSystem || candidate[pinned].Role == llm.RoleDeveloper) {
		pinned++
	}
	over := func() bool {
		return s.cfg.MaxMessages > 0 && len(candidate) > s.cfg.MaxMessages || s.cfg.MaxTokens > 0 && s.cfg.CountTokens(cloneMessages(candidate)) > s.cfg.MaxTokens
	}
	for over() {
		next := pinned + 1
		for next < len(candidate) && candidate[next].Role != llm.RoleUser {
			next++
		}
		if next >= len(candidate) {
			return ErrBudget
		}
		candidate = append(candidate[:pinned], candidate[next:]...)
	}
	s.messages = candidate
	return nil
}

type SequenceAllocator[K comparable] struct {
	mu     sync.Mutex
	values map[K]uint64
}

func (s *SequenceAllocator[K]) Next(key K) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[K]uint64{}
	}
	s.values[key]++
	return s.values[key]
}

// Forget is only safe once every in-flight update for the key has completed.
func (s *SequenceAllocator[K]) Forget(key K) { s.mu.Lock(); delete(s.values, key); s.mu.Unlock() }
