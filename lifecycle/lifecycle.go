// Package lifecycle coordinates long-running tasks and reverse-order cleanup.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var ErrShutdownTimeout = errors.New("lifecycle: shutdown deadline exceeded")
var ErrAlreadyStarted = errors.New("lifecycle: manager already started")

type Config struct{ ShutdownTimeout time.Duration } // zero defaults to 10s
type entry struct {
	name   string
	fn     func(context.Context) error
	budget time.Duration
}

// Manager is configured before Run and may only be run once. Registered tasks
// must cooperate with cancellation; Go cannot forcibly stop a goroutine.
type Manager struct {
	mu      sync.Mutex
	started bool
	tasks   []entry
	hooks   []entry
	quiesce []entry
	timeout time.Duration
}

func New(cfg Config) (*Manager, error) {
	if cfg.ShutdownTimeout < 0 {
		return nil, errors.New("lifecycle: shutdown timeout must not be negative")
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	return &Manager{timeout: cfg.ShutdownTimeout}, nil
}

// Add registers a long-running task. Any task's return, even nil, initiates shutdown.
func (m *Manager) Add(name string, task func(context.Context) error, options ...Option) error {
	return m.register(name, task, "task", options...)
}

// OnStop registers cleanup, called in reverse registration order after tasks exit.
func (m *Manager) OnStop(name string, hook func(context.Context) error, options ...Option) error {
	return m.register(name, hook, "stop", options...)
}

type Option func(*entry)

func WithStopTimeout(timeout time.Duration) Option { return func(e *entry) { e.budget = timeout } }

// OnQuiesce stops intake before task cancellation and draining.
func (m *Manager) OnQuiesce(name string, hook func(context.Context) error, options ...Option) error {
	return m.register(name, hook, "quiesce", options...)
}
func (m *Manager) register(name string, fn func(context.Context) error, phase string, options ...Option) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return ErrAlreadyStarted
	}
	if name == "" || fn == nil {
		return errors.New("lifecycle: name and function are required")
	}
	for _, e := range append(append(append([]entry(nil), m.tasks...), m.hooks...), m.quiesce...) {
		if e.name == name {
			return fmt.Errorf("lifecycle: duplicate name %q", name)
		}
	}
	e := entry{name: name, fn: fn}
	for _, o := range options {
		if o != nil {
			o(&e)
		}
	}
	if e.budget < 0 {
		return errors.New("lifecycle: negative stop budget")
	}
	if phase == "quiesce" {
		m.quiesce = append(m.quiesce, e)
	} else if phase == "stop" {
		m.hooks = append(m.hooks, e)
	} else {
		m.tasks = append(m.tasks, e)
	}
	return nil
}

// Run starts tasks concurrently and waits for cancellation or the first task exit.
// It quiesces intake, cancels tasks, waits for them, then calls cleanup hooks sequentially.
// A single fresh shutdown deadline covers draining and cleanup. Normal parent
// cancellation returns nil; task and cleanup failures are joined with errors.Join.
// On timeout, outstanding goroutines may continue and remaining hooks are skipped.
func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("lifecycle: context is required")
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return ErrAlreadyStarted
	}
	m.started = true
	tasks, hooks := append([]entry(nil), m.tasks...), append([]entry(nil), m.hooks...)
	m.mu.Unlock()
	workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	done := make(chan error, len(tasks))
	started := 0
	if ctx.Err() == nil {
		for _, task := range tasks {
			started++
			go func(e entry) {
				err := runTask(workCtx, e)
				if cancellationOnly(err, workCtx.Err()) {
					err = nil
				}
				done <- err
			}(task)
		}
	}
	var failures []error
	collect := func(err error) {
		if err == nil {
			return
		}
		failures = append(failures, err)
	}
	remaining := started
	select {
	case <-ctx.Done():
	case err := <-done:
		remaining--
		collect(err)
	}
	stopCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), m.timeout)
	defer stop()
	for i := len(m.quiesce) - 1; i >= 0; i-- {
		collect(runHook(stopCtx, m.quiesce[i]))
	}
	cancel()
	for remaining > 0 {
		select {
		case err := <-done:
			remaining--
			collect(err)
		case <-stopCtx.Done():
			return errors.Join(append(failures, ErrShutdownTimeout)...)
		}
	}
	for i := len(hooks) - 1; i >= 0; i-- {
		if stopCtx.Err() != nil {
			return errors.Join(append(failures, ErrShutdownTimeout)...)
		}
		collect(runHook(stopCtx, hooks[i]))
	}
	if stopCtx.Err() != nil {
		failures = append(failures, ErrShutdownTimeout)
	}
	return errors.Join(failures...)
}

// Ignore only cancellation caused by this manager, preserving any additional
// failures in joined errors and independent task deadlines.
func cancellationOnly(err, cause error) bool {
	if err == nil || cause == nil {
		return false
	}
	if err == cause {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !cancellationOnly(child, cause) {
				return false
			}
		}
		return true
	}
	return cancellationOnly(errors.Unwrap(err), cause)
}

// RunSignals additionally handles SIGINT and SIGTERM by default. Supplying
// signals replaces those defaults. Signal registration is released on return.
func (m *Manager) RunSignals(ctx context.Context, signals ...os.Signal) error {
	if ctx == nil {
		return errors.New("lifecycle: context is required")
	}
	if len(signals) == 0 {
		signals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	}
	ctx, stop := signal.NotifyContext(ctx, signals...)
	defer stop()
	return m.Run(ctx)
}

func invoke(ctx context.Context, e entry) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("lifecycle: %s panicked: %v", e.name, recovered)
		}
	}()
	if err := e.fn(ctx); err != nil {
		return fmt.Errorf("lifecycle: %s: %w", e.name, err)
	}
	return nil
}

// Budgets bound waiting, not goroutine execution; tasks must honor cancellation.
func runTask(ctx context.Context, e entry) error {
	if e.budget == 0 {
		return invoke(ctx, e)
	}
	result := make(chan error, 1)
	go func() { result <- invoke(ctx, e) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
	}
	timer := time.NewTimer(e.budget)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return fmt.Errorf("lifecycle: %s: %w", e.name, ErrShutdownTimeout)
	}
}
func runHook(ctx context.Context, e entry) error {
	hookCtx := ctx
	cancel := func() {}
	if e.budget > 0 {
		hookCtx, cancel = context.WithTimeout(ctx, e.budget)
	}
	defer cancel()
	if hookCtx.Err() != nil {
		return ErrShutdownTimeout
	}
	result := make(chan error, 1)
	go func() { result <- invoke(hookCtx, e) }()
	select {
	case err := <-result:
		return err
	case <-hookCtx.Done():
		return fmt.Errorf("lifecycle: %s: %w", e.name, ErrShutdownTimeout)
	}
}
