// Package nplevent implements the nonblocking event handoff portion of a C6
// execution port. It is not a controller, native task scheduler or BLE backend.
package nplevent

import (
	"errors"
	"time"
)

var (
	ErrArgument    = errors.New("nplevent: invalid argument")
	ErrISR         = errors.New("nplevent: foreground operation called from interrupt")
	ErrUnsupported = errors.New("nplevent: blocking or native-task operation unsupported")
	ErrClosed      = errors.New("nplevent: closed")
	ErrFull        = errors.New("nplevent: queue full")
	ErrBusy        = errors.New("nplevent: event or dispatcher still owned")
	ErrAborted     = errors.New("nplevent: callback did not return normally")
)

// Event keeps its callback reachable until Release succeeds. A callback runs
// only on the foreground caller's existing Go task, with interrupts restored.
type Event struct {
	noCopy   noCopy
	callback func() error
	onCancel func() error
	queued   *Queue
	running  *Queue
	released bool
}

// Queue has one foreground dispatcher and fixed pending-event capacity. Queue
// and Event values must be constructed with New/NewEvent and never copied.
type Queue struct {
	noCopy      noCopy
	events      []*Event
	head, count int
	running     bool
	closing     bool
	joined      bool
	done        chan struct{}
	firstError  error
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// New allocates only during foreground setup. Capacity is an explicit service
// resource limit, not a supported BLE-controller configuration or memory claim.
func New(capacity int) (*Queue, error) {
	if inISR() {
		return nil, ErrISR
	}
	if capacity < 1 || capacity > int(^uint(0)>>1)/pointerBytes {
		return nil, ErrArgument
	}
	return &Queue{events: make([]*Event, capacity), done: make(chan struct{})}, nil
}

func NewEvent(callback func() error) (*Event, error) {
	return NewEventWithCancel(callback, nil)
}

// NewEventWithCancel adds a bounded cancellation hook. It runs under the queue
// critical section before detaching an occurrence, must not re-enter Go
// services/block/allocate, and must support the caller's context. A rejected
// cancellation retains ownership and keeps a closing queue unjoined.
func NewEventWithCancel(callback func() error, cancel func() error) (*Event, error) {
	if inISR() {
		return nil, ErrISR
	}
	if callback == nil {
		return nil, ErrArgument
	}
	return &Event{callback: callback, onCancel: cancel}, nil
}

// Put performs no allocation or scheduler call on C6. Duplicate insertion of
// an already queued event is suppressed, as in the cached NPL. It does not wake
// a task; the foreground owner must arrange dispatch.
func (q *Queue) Put(event *Event) error {
	if q == nil || q.done == nil || event == nil {
		return ErrArgument
	}
	key := lock()
	defer unlock(key)
	if q.closing || event.released {
		return ErrClosed
	}
	if event.callback == nil {
		return ErrArgument
	}
	if event.queued != nil {
		if event.queued == q {
			return nil
		}
		return ErrBusy
	}
	if event.running != nil && event.running != q {
		return ErrBusy
	}
	if q.count == len(q.events) {
		return ErrFull
	}
	q.events[(q.head+q.count)%len(q.events)] = event
	q.count++
	event.queued = q
	return nil
}

// Remove cancels only a queued occurrence. An executing callback is never
// killed or treated as complete; Release and queue join retain that owner.
func (q *Queue) Remove(event *Event) (bool, error) {
	if q == nil || q.done == nil || event == nil {
		return false, ErrArgument
	}
	key := lock()
	defer unlock(key)
	if event.queued != q {
		return false, nil
	}
	if err := q.cancelLocked(event); err != nil {
		return false, err
	}
	position := 0
	for q.events[(q.head+position)%len(q.events)] != event {
		position++
	}
	for i := position; i < q.count-1; i++ {
		q.events[(q.head+i)%len(q.events)] = q.events[(q.head+i+1)%len(q.events)]
	}
	q.events[(q.head+q.count-1)%len(q.events)] = nil
	q.count--
	event.queued = nil
	return true, nil
}

// Dispatch executes at most one callback on the calling Go task. Only a
// nonblocking timeout is supported: native C task blocking/join is NOT supplied.
// The bool distinguishes an empty queue from an actual callback result.
func (q *Queue) Dispatch(timeout time.Duration) (ran bool, err error) {
	if q == nil || q.done == nil {
		return false, ErrArgument
	}
	if inISR() {
		return false, ErrISR
	}
	if timeout != 0 {
		return false, ErrUnsupported
	}
	key := lock()
	if q.closing {
		unlock(key)
		return false, ErrClosed
	}
	if q.running {
		unlock(key)
		return false, ErrBusy
	}
	if q.count == 0 {
		unlock(key)
		return false, nil
	}
	event := q.events[q.head]
	q.events[q.head] = nil
	q.head = (q.head + 1) % len(q.events)
	q.count--
	q.running = true
	event.queued = nil
	event.running = q
	callback := event.callback
	unlock(key)

	result := ErrAborted
	defer func() {
		key := lock()
		if result != nil && q.firstError == nil {
			q.firstError = result
		}
		event.running = nil
		q.running = false
		q.finishLocked()
		unlock(key)
	}()
	result = callback()
	return true, result
}

// Close seals admission and removes pending occurrences. Done closes only
// after the current callback really returns/unwinds. ErrBusy keeps ownership
// explicit while that callback is outstanding, even if its queue is empty.
func (q *Queue) Close() error {
	if q == nil || q.done == nil {
		return ErrArgument
	}
	if inISR() {
		return ErrISR
	}
	key := lock()
	defer unlock(key)
	q.closing = true
	for q.count != 0 {
		event := q.events[q.head]
		if err := q.cancelLocked(event); err != nil {
			return err
		}
		q.events[q.head] = nil
		q.head = (q.head + 1) % len(q.events)
		q.count--
		event.queued = nil
	}
	q.finishLocked()
	if q.running {
		return ErrBusy
	}
	return nil
}

func (q *Queue) cancelLocked(event *Event) error {
	if event.onCancel != nil {
		if err := event.onCancel(); err != nil {
			if q.firstError == nil {
				q.firstError = err
			}
			return err
		}
	}
	return nil
}

func (q *Queue) finishLocked() {
	if q.closing && q.count == 0 && !q.running && !q.joined {
		q.joined = true
		close(q.done)
	}
}

func (q *Queue) Done() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.done
}

// Error retains the first actual callback failure, independently of resource
// join. It is not a readiness flag and never prevents genuine completion.
func (q *Queue) Error() error {
	if q == nil || q.done == nil {
		return ErrArgument
	}
	key := lock()
	defer unlock(key)
	return q.firstError
}

func (event *Event) Release() error {
	if event == nil {
		return ErrArgument
	}
	if inISR() {
		return ErrISR
	}
	key := lock()
	defer unlock(key)
	if event.queued != nil || event.running != nil {
		return ErrBusy
	}
	if event.callback == nil && !event.released {
		return ErrArgument
	}
	event.callback = nil
	event.onCancel = nil
	event.released = true
	return nil
}
