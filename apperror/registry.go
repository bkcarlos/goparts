package apperror

import (
	"errors"
	"fmt"
	"sync"
)

type Mapping struct{ HTTP, Exit int }
type Definition struct {
	Code, Message, Guidance string
	Mapping                 Mapping
}
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
}

func NewRegistry() *Registry { return &Registry{definitions: map[string]Definition{}} }
func (r *Registry) Register(d Definition) error {
	if d.Code == "" || d.Message == "" || d.Mapping.HTTP != 0 && (d.Mapping.HTTP < 100 || d.Mapping.HTTP > 599) || d.Mapping.Exit < 0 || d.Mapping.Exit > 255 {
		return errors.New("apperror: invalid definition or mapping")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.definitions == nil {
		r.definitions = map[string]Definition{}
	}
	if _, ok := r.definitions[d.Code]; ok {
		return fmt.Errorf("apperror: duplicate code %q", d.Code)
	}
	r.definitions[d.Code] = d
	return nil
}
func (r *Registry) Lookup(code string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.definitions[code]
	return d, ok
}
func (r *Registry) New(code string, opts ...Option) (*Base, error) {
	d, ok := r.Lookup(code)
	if !ok {
		return nil, fmt.Errorf("apperror: unregistered code %q", code)
	}
	if d.Guidance != "" {
		opts = append([]Option{WithFields(Fields{"guidance": d.Guidance})}, opts...)
	}
	return New(d.Code, d.Message, opts...), nil
}
func (r *Registry) mapping(err error) Mapping {
	var found Mapping
	walkErrors(err, func(e error) bool {
		if c, ok := e.(Coded); ok {
			code, _ := c.ErrorInfo()
			if d, exists := r.Lookup(code); exists {
				found = d.Mapping
				return false
			}
		}
		return true
	})
	return found
}
func (r *Registry) HTTPStatus(err error) int {
	if err == nil {
		return 200
	}
	if m := r.mapping(err); m.HTTP != 0 {
		return m.HTTP
	}
	return 500
}
func (r *Registry) ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if m := r.mapping(err); m.Exit != 0 {
		return m.Exit
	}
	return 1
}

var defaultRegistry = NewRegistry()

func RegisterMapping(code string, mapping Mapping) error {
	return defaultRegistry.Register(Definition{Code: code, Message: code, Mapping: mapping})
}
func HTTPStatus(err error) int { return defaultRegistry.HTTPStatus(err) }
func ExitCode(err error) int   { return defaultRegistry.ExitCode(err) }

// DescribeAll reports each classified branch of a joined error. An outer coded
// wrapper classifies that branch; its cause is not reported again.
func DescribeAll(err error) []Record {
	var result []Record
	var visit func(error, int)
	visit = func(e error, depth int) {
		if e == nil || depth > 1024 {
			return
		}
		if _, ok := e.(Coded); ok {
			result = append(result, *Describe(e))
			return
		}
		if multi, ok := e.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				visit(child, depth+1)
			}
			return
		}
		if single, ok := e.(interface{ Unwrap() error }); ok && single.Unwrap() != nil {
			visit(single.Unwrap(), depth+1)
			return
		}
		result = append(result, *Describe(e))
	}
	visit(err, 0)
	return result
}
func walkErrors(err error, fn func(error) bool) {
	stack := []error{err}
	for steps := 0; len(stack) > 0 && steps < 4096; steps++ {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e == nil {
			continue
		}
		if !fn(e) {
			return
		}
		switch v := e.(type) {
		case interface{ Unwrap() []error }:
			children := v.Unwrap()
			for i := len(children) - 1; i >= 0; i-- {
				stack = append(stack, children[i])
			}
		case interface{ Unwrap() error }:
			stack = append(stack, v.Unwrap())
		}
	}
}
