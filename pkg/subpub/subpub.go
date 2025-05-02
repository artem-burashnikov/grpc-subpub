// Package subpub provides a simple in-memory publish-subscribe mechanism.
// It supports asynchronous message delivery with FIFO ordering per subscriber.
// A slow subscriber does not block others.
package subpub

import (
	"context"
	"errors"
	"sync"
)

// ErrClosed is returned when Publish or Subscribe is called after Close.
var ErrClosed = errors.New("subpub system is closed")

// MessageHandler is a callback function that processes messages delivered to subscribers.
type MessageHandler func(msg any)

// Subscription represents a subscriber's active interest in a subject.
type Subscription interface {
	// Unsibscribe will remove interest in the current subject subscription is for.
	Unsibscribe()
}

// A SubPub instance allows clients to subscribe to string-based subjects, publish messages, and shut down the system.
type SubPub interface {
	// Subscribe creates an asynchronous queue subscriber on the given subject.
	Subscribe(subject string, cb MessageHandler) (Subscription, error)

	// Publish publishes the msg argument to the given subject.
	Publish(subject string, msg any) error

	// Close will shutdown sub-pub system.
	// May be blocked by data delivery until the context is canceled.
	Close(ctx context.Context) error
}

// New creates and returns a new SubPub instance.
func New() SubPub {
	return &subPub{
		subs: make(map[string]map[*subscription]struct{}),
	}
}

// subPub is an in-memory event bus that manages subscriptions and message delivery.
type subPub struct {
	mu     sync.RWMutex                          // protects subs map and closed flag
	subs   map[string]map[*subscription]struct{} // subject -> set of subscriptions
	closed bool                                  // indicates whether Close has been called
	wg     sync.WaitGroup                        // tracks active listener goroutines
}

// subscription holds state for a single subscriber on a particular subject.
type subscription struct {
	eventBus *subPub        // reference back to pub‑sub system for cleanup
	subject  string         // topic this subscription listens to
	handler  MessageHandler // callback for incoming messages

	queueMu sync.Mutex // protects queue and closed flag
	cond    *sync.Cond // signals when new messages arrive or subscription closes
	queue   []any      // unbounded FIFO queue of pending messages
	closed  bool       // indicates whether this subscription has been unsubscribed
}

// Subscribe registers a handler for the given subject and starts its listener goroutine.
func (s *subPub) Subscribe(subject string, cb MessageHandler) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, ErrClosed
	}

	sub := &subscription{
		eventBus: s,
		subject:  subject,
		handler:  cb,
		queue:    make([]any, 0),
	}
	sub.cond = sync.NewCond(&sub.queueMu)

	if s.subs[subject] == nil {
		s.subs[subject] = make(map[*subscription]struct{})
	}
	s.subs[subject][sub] = struct{}{}

	s.wg.Add(1)
	go sub.listen()

	return sub, nil
}

// enqueue adds a message to the subscription's queue and notifies the listener.
func (sub *subscription) enqueue(msg any) {
	sub.queueMu.Lock()
	if !sub.closed {
		sub.queue = append(sub.queue, msg)
		sub.cond.Signal() // wake up one waiting listener
	}
	sub.queueMu.Unlock()
}

// listen runs in its own goroutine to process queued messages in FIFO order.
func (sub *subscription) listen() {
	defer sub.eventBus.wg.Done()
	for {
		sub.queueMu.Lock()
		// Wait until there is a message or subscription is closed.
		for len(sub.queue) == 0 && !sub.closed {
			sub.cond.Wait()
		}
		// Exit if unsubscribed was called and queue is drained.
		if len(sub.queue) == 0 && sub.closed {
			sub.queueMu.Unlock()
			return
		}
		// Dequeue next message.
		msg := sub.queue[0]
		sub.queue = sub.queue[1:]
		sub.queueMu.Unlock()

		// Invoke handler outside the lock to avoid blocking enqueue.
		sub.handler(msg)
	}
}

// Publish sends the message to all subscribers of the subject.
//
// Slow subscribers won't block the publisher since this method doesn't wait on handlers, it just enques the message.
func (s *subPub) Publish(subject string, msg any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrClosed
	}

	for sub := range s.subs[subject] {
		sub.enqueue(msg)
	}

	return nil
}

// Unsibscribe removes the subscription and signals its listeners to exit.
func (sub *subscription) Unsibscribe() {
	eb := sub.eventBus

	// Remove the subscription from eventBus.
	eb.mu.Lock()
	delete(eb.subs[sub.subject], sub)
	if len(eb.subs[sub.subject]) == 0 {
		delete(eb.subs, sub.subject)
	}
	eb.mu.Unlock()

	// Signal listeners to terminate after draining queue.
	sub.queueMu.Lock()
	sub.closed = true
	sub.cond.Broadcast() // wake up all listeners that are waiting
	sub.queueMu.Unlock()
}

// Close shuts down the pub‑sub system, unsubscribes all subscribers, and waits util all handlers are done or the context is canceled.
func (s *subPub) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	// Mark the system as `closed` and send a signal to all active subscriptions.
	s.closed = true
	for _, subs := range s.subs {
		for sub := range subs {
			sub.queueMu.Lock()
			sub.closed = true
			sub.cond.Broadcast()
			sub.queueMu.Unlock()
		}
	}
	s.subs = nil
	s.mu.Unlock()

	// Wait for all listener goroutines or context cancellation.
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
