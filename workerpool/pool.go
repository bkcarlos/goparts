// Package workerpool bounds queued and running work. Shutdown drains accepted
// work; task functions must honor their context. Submit does not await execution.
package workerpool

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrClosed = errors.New("workerpool: closed")
var ErrPanic = errors.New("workerpool: task panicked")

type Task func(context.Context) error
type Event struct {
	Wait, Duration time.Duration
	Err            error
}
type job struct {
	ctx    context.Context
	fn     Task
	queued time.Time
}
type Pool struct {
	mu      sync.RWMutex
	closing chan struct{}
	done    chan struct{}
	jobs    chan job
	once    sync.Once
	workers sync.WaitGroup
	observe func(Event)
}

func New(size, queueLen int, observer ...func(Event)) (*Pool, error) {
	if size < 1 || queueLen < 0 {
		return nil, errors.New("workerpool: invalid capacity")
	}
	p := &Pool{closing: make(chan struct{}), done: make(chan struct{}), jobs: make(chan job, queueLen)}
	if len(observer) > 0 {
		p.observe = observer[0]
	}
	for i := 0; i < size; i++ {
		p.workers.Add(1)
		go func() {
			defer p.workers.Done()
			for j := range p.jobs {
				run(j, p.observe)
			}
		}()
	}
	return p, nil
}
func run(j job, observe func(Event)) {
	start := time.Now()
	err := func() (err error) {
		defer func() {
			if recover() != nil {
				err = ErrPanic
			}
		}()
		if err = j.ctx.Err(); err != nil {
			return err
		}
		return j.fn(j.ctx)
	}()
	if observe != nil {
		observe(Event{Wait: start.Sub(j.queued), Duration: time.Since(start), Err: err})
	}
}
func (p *Pool) Submit(ctx context.Context, fn Task) error {
	if ctx == nil || fn == nil {
		return errors.New("workerpool: context/task required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	select {
	case <-p.closing:
		return ErrClosed
	default:
	}
	select {
	case <-p.closing:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case p.jobs <- job{ctx, fn, time.Now()}:
		return nil
	}
}
func (p *Pool) Close() {
	p.once.Do(func() {
		close(p.closing)
		p.mu.Lock()
		close(p.jobs)
		p.mu.Unlock()
		go func() { p.workers.Wait(); close(p.done) }()
	})
}
func (p *Pool) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return nil
	}
}

// Stream schedules keys fairly without occupying a worker while waiting for a
// previous job with the same key. Accepted order per key is FIFO.
type Stream[K comparable] struct {
	mu      sync.Mutex
	queues  map[K][]job
	ready   chan K
	slots   chan struct{}
	closing chan struct{}
	done    chan struct{}
	pending sync.WaitGroup
	workers sync.WaitGroup
	once    sync.Once
	observe func(Event)
}

func NewStream[K comparable](size, queueLen int, observer ...func(Event)) (*Stream[K], error) {
	if size < 1 || queueLen < 0 {
		return nil, errors.New("workerpool: invalid capacity")
	}
	s := &Stream[K]{queues: map[K][]job{}, ready: make(chan K, size+queueLen), slots: make(chan struct{}, size+queueLen), closing: make(chan struct{}), done: make(chan struct{})}
	if len(observer) > 0 {
		s.observe = observer[0]
	}
	for i := 0; i < size; i++ {
		s.workers.Add(1)
		go s.worker()
	}
	return s, nil
}
func (s *Stream[K]) Submit(ctx context.Context, key K, fn Task) error {
	if ctx == nil || fn == nil {
		return errors.New("workerpool: context/task required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closing:
		return ErrClosed
	case s.slots <- struct{}{}:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closing:
		<-s.slots
		return ErrClosed
	default:
	}
	_, active := s.queues[key]
	s.queues[key] = append(s.queues[key], job{ctx, fn, time.Now()})
	s.pending.Add(1)
	if !active {
		s.ready <- key
	}
	return nil
}
func (s *Stream[K]) worker() {
	defer s.workers.Done()
	for key := range s.ready {
		s.mu.Lock()
		j := s.queues[key][0]
		s.queues[key] = s.queues[key][1:]
		s.mu.Unlock()
		run(j, s.observe)
		s.mu.Lock()
		if len(s.queues[key]) > 0 {
			s.ready <- key
		} else {
			delete(s.queues, key)
		}
		<-s.slots
		s.pending.Done()
		s.mu.Unlock()
	}
}
func (s *Stream[K]) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		close(s.closing)
		s.mu.Unlock()
		go func() { s.pending.Wait(); close(s.ready); s.workers.Wait(); close(s.done) }()
	})
}
func (s *Stream[K]) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return nil
	}
}
