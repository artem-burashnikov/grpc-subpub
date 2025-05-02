package subpub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClose(t *testing.T) {
	bus := New()

	_, _ = bus.Subscribe("foo", func(msg any) {})

	err := bus.Close(context.Background())
	assert.NoError(t, err)
}

func TestCloseWithDeadline(t *testing.T) {
	bus := New()

	_, _ = bus.Subscribe("foo", func(msg any) {
		time.Sleep(1 * time.Second)
	})

	_ = bus.Publish("foo", "bar")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := bus.Close(ctx)
	duration := time.Since(start)

	assert.True(t, errors.Is(err, context.DeadlineExceeded), "expected context.DeadlineExceeded, got %v", err)
	assert.LessOrEqual(t, duration, 200*time.Millisecond, "Close took too long: %v", duration)
}

func TestSubscribeAfterClose(t *testing.T) {
	bus := New()
	_ = bus.Close(context.Background())

	_, err := bus.Subscribe("foo", func(msg any) {})
	assert.Equal(t, ErrClosed, err)
}

func TestPublishAfterClose(t *testing.T) {
	bus := New()
	_ = bus.Close(context.Background())

	err := bus.Publish("foo", "bar")
	assert.Equal(t, ErrClosed, err)
}

func TestDoubleClose(t *testing.T) {
	bus := New()

	err := bus.Close(context.Background())
	assert.NoError(t, err)

	err = bus.Close(context.Background())
	assert.Equal(t, ErrClosed, err)
}

func TestCloseWithMultipleSubscribers(t *testing.T) {
	bus := New()

	_, _ = bus.Subscribe("foo", func(msg any) {})
	_, _ = bus.Subscribe("bar", func(msg any) {})
	_, _ = bus.Subscribe("baz", func(msg any) {})

	err := bus.Close(context.Background())
	assert.NoError(t, err)
}
