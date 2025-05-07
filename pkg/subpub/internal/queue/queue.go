package queue

// Inspired by https://drive.google.com/file/d/1nPdvhB0PutEJzdCq5ms6UI58dp50fcAN/view (last accessed: 2025-05-06)

type Queue struct {
	Items chan []any    // non-empty slices only
	Empty chan struct{} // holds value if the queue is empty
}

func New() *Queue {
	items := make(chan []any, 1)
	empty := make(chan struct{}, 1)
	empty <- struct{}{}
	return &Queue{items, empty}
}

func (q *Queue) Dequeue() (any, bool) {
	var items []any
	select {
	case items = <-q.Items:
	default:
		return nil, false
	}

	item := items[0]
	items = items[1:]

	if len(items) == 0 {
		q.Empty <- struct{}{}
	} else {
		q.Items <- items
	}

	return item, true
}

func (q *Queue) Enqueue(item any) {
	var items []any
	select {
	case items = <-q.Items:
	case <-q.Empty:
	}
	items = append(items, item)
	q.Items <- items
}
