package subpub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestClose(t *testing.T) {
	bus := New()

	bus.Subscribe("foo", func(msg any) {})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := bus.Close(ctx)
	require.Nil(t, err)
}

func TestCloseWithDeadline(t *testing.T) {
	bus := New()

	done := make(chan int)

	bus.Subscribe("foo", func(msg any) {
		time.Sleep(1 * time.Second)
		done <- 42
	})

	bus.Publish("foo", "bar")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := bus.Close(ctx)
	duration := time.Since(start)

	require.Equal(t, context.DeadlineExceeded, err, "expected context.DeadlineExceeded, got %v", err)
	require.LessOrEqual(t, duration, 200*time.Millisecond, "Close took too long: %v", duration)
	require.Equal(t, 42, <-done)
}

func TestSubscribeAfterClose(t *testing.T) {
	bus := New()

	err := bus.Close(context.Background())
	require.Nil(t, err)

	_, err = bus.Subscribe("foo", func(msg any) {})
	require.Equal(t, ErrClosed, err)
}

func TestPublishAfterClose(t *testing.T) {
	bus := New()

	bus.Close(context.Background())

	err := bus.Publish("foo", "bar")

	require.Equal(t, ErrClosed, err)
}

func TestDoubleClose(t *testing.T) {
	bus := New()

	require := require.New(t)

	err := bus.Close(context.Background())
	require.Nil(err)

	err = bus.Close(context.Background())
	require.Equal(ErrClosed, err)
}

func TestCloseWithMultipleSubscribers(t *testing.T) {
	bus := New()

	require := require.New(t)

	_, err := bus.Subscribe("foo", func(msg any) {})
	require.Nil(err)

	_, err = bus.Subscribe("bar", func(msg any) {})
	require.Nil(err)

	_, err = bus.Subscribe("baz", func(msg any) {})
	require.Nil(err)

	err = bus.Close(context.Background())
	require.NoError(err)
}

func TestConcurrentSubscribe(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	require := require.New(t)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sub1, err := sp.Subscribe("foo", func(msg any) {})
		require.NotNil(sub1)
		require.Nil(err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sub2, err := sp.Subscribe("bar", func(msg any) {})
		require.NotNil(sub2)
		require.Nil(err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sub3, err := sp.Subscribe("baz", func(msg any) {})
		require.NotNil(sub3)
		require.Nil(err)
	}()

	wg.Wait()
}

func TestSubscribePublish(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	require := require.New(t)

	done := make(chan string)

	var received string

	sp.Subscribe("foo", func(msg any) {
		defer close(done)
		received = msg.(string)
	})

	err := sp.Publish("foo", "bar")
	require.Nil(err)

	<-done
	require.Equal("bar", received)
}

func TestUnsubscribe(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	require := require.New(t)

	done := make(chan struct{})

	var calls = 0

	sub, err := sp.Subscribe("foo", func(msg any) {
		defer close(done)
		calls++
	})
	require.Nil(err)

	err = sp.Publish("foo", "bar")
	require.Nil(err)

	sub.Unsubscribe()

	err = sp.Publish("foo", "baz")
	require.Nil(err)

	<-done
	require.Equal(1, calls)
}

func TestUnsubscribeAfterClose(t *testing.T) {
	sp := New()

	assert := assert.New(t)

	sub, err := sp.Subscribe("foo", func(msg any) {})
	assert.Nil(err)

	err = sp.Close(context.Background())
	assert.Nil(err)

	assert.NotPanics(func() { sub.Unsubscribe() })
}

func TestDoubleUnsubscriber(t *testing.T) {
	sp := New()

	assert := assert.New(t)

	sub, err := sp.Subscribe("foo", func(msg any) {})
	assert.Nil(err)

	err = sp.Close(context.Background())
	assert.Nil(err)

	sub.Unsubscribe()

	assert.NotPanics(func() { sub.Unsubscribe() })
}

func TestConcurrentPublish(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	assert := assert.New(t)

	var (
		received sync.Map
		wg       sync.WaitGroup
	)

	sp.Subscribe("foo", func(msg any) {
		defer wg.Done()
		received.Store(msg, true)
	})

	const numMessages = 1000
	wg.Add(numMessages)

	for i := range numMessages {
		go func(i int) {
			err := sp.Publish("foo", i)
			assert.Nil(err)
		}(i)
	}

	wg.Wait()

	for i := range numMessages {
		_, ok := received.Load(i)
		assert.True(ok, "Message %d not received", i)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	assert := assert.New(t)

	var (
		sub1Count atomic.Int32
		sub2Count atomic.Int32
		wg        sync.WaitGroup
	)

	sub1, err := sp.Subscribe("foo", func(msg any) {
		defer wg.Done()
		sub1Count.Add(1)
	})
	assert.Nil(err)

	_, err = sp.Subscribe("foo", func(msg any) {
		defer wg.Done()
		sub2Count.Add(1)
	})
	assert.Nil(err)

	wg.Add(2)
	err = sp.Publish("foo", "bar")
	assert.Nil(err)

	wg.Wait()
	assert.Equal(int32(1), sub1Count.Load())
	assert.Equal(int32(1), sub2Count.Load())

	sub1.Unsubscribe()

	wg.Add(1)
	err = sp.Publish("foo", "baz")
	require.NoError(t, err)

	wg.Wait()
	assert.Equal(int32(1), sub1Count.Load(), "Sub1 should not receive after unsubscribe")
	assert.Equal(int32(2), sub2Count.Load(), "Sub2 should receive both messages")
}

func TestPanicInHandler(t *testing.T) {
	sp := New()
	defer sp.Close(context.Background())

	require := require.New(t)

	var (
		received []string
		mu       sync.Mutex
	)

	count := 0

	_, err := sp.Subscribe("foo", func(msg any) {
		mu.Lock()
		defer mu.Unlock()

		count++
		if count == 2 {
			panic("intentional panic")
		}
		received = append(received, msg.(string))
	})
	require.NoError(err)

	for range 2 {
		err = sp.Publish("foo", "bar")
		require.NoError(err)
	}

	err = sp.Publish("foo", "baz")
	require.NoError(err)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, len(received))
	mu.Unlock()
}
