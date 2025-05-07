package queue

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestDestroyNonEmptyQueue(t *testing.T) {
	q := New()
	q.Enqueue(1)
}

func TestDequeueEnqueueDequeue(t *testing.T) {
	q := New()

	require := require.New(t)

	_, ok := q.Dequeue()
	require.False(ok, "Dequeue from empty queue should return false")

	q.Enqueue(42)
	val, ok := q.Dequeue()
	require.True(ok, "Dequeue should succeed")
	require.Equal(42, val, "Dequeued value should match")

	_, ok = q.Dequeue()
	require.False(ok, "Queue should be empty again")
}

func TestEnqueueDequeue(t *testing.T) {
	q := New()

	assert := assert.New(t)

	const numItems = 1000
	for i := range numItems {
		q.Enqueue(i)
	}

	for i := range numItems {
		val, ok := q.Dequeue()
		assert.True(ok)
		assert.Equal(i, val)
	}

	_, ok := q.Dequeue()
	assert.Equal(false, ok, "queue should be empty")
}

func TestQueueConcurrent(t *testing.T) {
	q := New()

	require := require.New(t)

	const numItems = 1000

	results := make(chan int, numItems)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range numItems {
			q.Enqueue(i)
		}
	}()

	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(results)
		for range numItems {
			val, ok := q.Dequeue()
			require.True(ok)
			results <- val.(int)
		}
	}()

	wg.Wait()

	seen := make(map[int]bool)
	for val := range results {
		seen[val] = true
	}

	for i := range numItems {
		require.True(seen[i], "All items should be received")
	}
}
