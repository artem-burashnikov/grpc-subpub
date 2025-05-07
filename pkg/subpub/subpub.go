package subpub

import (
	"context"
	"errors"
	"log"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/artem-burashnikov/grpc-subpub/pkg/subpub/internal/queue"
)

// ErrClosed is returned when Publish, Subscribe or Close is called after Close.
var ErrClosed = errors.New("subpub system is closed")

// MessageHandler is a callback function that processes messages delivered to subscribers.
type MessageHandler func(msg any)

// Subscription represents a subscriber's active interest in a subject.
type Subscription interface {
	// Unsibscribe will remove interest in the current subject subscription is for.
	Unsubscribe()
}

type SubPub interface {
	// Subscribe creates an asynchronous queue subscriber on the given subject.
	Subscribe(subject string, cb MessageHandler) (Subscription, error)

	// Publish publishes the msg argument to the given subject.
	Publish(subject string, msg any) error

	// Close will shutdown sub-pub system.
	// May be blocked by data delivery until the context is canceled.
	Close(ctx context.Context) error
}

func New() SubPub {
	return &subPub{
		subs: make(map[string][]chan any),
	}
}

type subPub struct {
	mu     sync.RWMutex
	subs   map[string][]chan any
	closed bool
	wg     sync.WaitGroup
}

type subscription struct {
	subpub            *subPub  // parent sub-pub system
	bus               chan any // message bus
	subject           string   // subject the subscription is for
	handler           MessageHandler
	localRunningQueue *queue.Queue // local message queue
	closed            atomic.Bool
	// wg                sync.WaitGroup // number of active subscriptions
}

func (s *subPub) Subscribe(subject string, cb MessageHandler) (Subscription, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	defer s.mu.Unlock()

	sub := &subscription{
		subpub:            s,
		bus:               make(chan any, 1),
		subject:           subject,
		handler:           cb,
		localRunningQueue: queue.New(),
	}

	s.subs[subject] = append(s.subs[subject], sub.bus)

	s.wg.Add(1)
	go sub.listen()

	return sub, nil
}

func (sub *subscription) listen() {
	defer sub.subpub.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})

	go sub.processMessages(ctx, done)

	for msg := range sub.bus {
		sub.localRunningQueue.Enqueue(msg)
	}

	cancel()
	<-done

	sub.drainQueue()
}

func (sub *subscription) drainQueue() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in drainQueue: %v\nstack: %s", r, debug.Stack())
		}
	}()

	for {
		msg, ok := sub.localRunningQueue.Dequeue()
		if !ok {
			break
		}
		sub.handler(msg)
	}
}

func (sub *subscription) processMessages(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in processMessages: %v\nstack: %s", r, debug.Stack())
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if msg, ok := sub.localRunningQueue.Dequeue(); ok {
				sub.handler(msg)
			}
		}
	}
}

func (s *subPub) Publish(subject string, msg any) error {
	s.mu.RLock()

	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}

	defer s.mu.RUnlock()

	for _, sub := range s.subs[subject] {
		sub <- msg
	}

	return nil
}

func (sub *subscription) Unsubscribe() {
	if sub.closed.Load() {
		return
	}
	sub.closed.Store(true)

	sub.subpub.mu.Lock()
	defer sub.subpub.mu.Unlock()
	for i, bus := range sub.subpub.subs[sub.subject] {
		if bus == sub.bus {
			sub.subpub.subs[sub.subject] = slices.Delete(sub.subpub.subs[sub.subject], i, i+1)
			close(sub.bus)
			break
		}
	}
}

func (s *subPub) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}

	for _, subs := range s.subs {
		for _, sub := range subs {
			close(sub)
		}
	}

	s.closed = true
	s.subs = nil
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Ensure types implement interfaces.
var _ Subscription = (*subscription)(nil)
var _ SubPub = (*subPub)(nil)
