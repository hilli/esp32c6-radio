package nplevent

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func queue(t *testing.T, capacity int) *Queue {
	t.Helper()
	q, err := New(capacity)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func event(t *testing.T, fn func() error) *Event {
	t.Helper()
	ev, err := NewEvent(fn)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func mustPut(t *testing.T, q *Queue, ev *Event) {
	t.Helper()
	if err := q.Put(ev); err != nil {
		t.Fatal(err)
	}
}

func mustDispatch(t *testing.T, q *Queue) {
	t.Helper()
	if ran, err := q.Dispatch(0); !ran || err != nil {
		t.Fatalf("dispatch = %t, %v", ran, err)
	}
}

func requireDone(t *testing.T, q *Queue, want bool) {
	t.Helper()
	select {
	case <-q.Done():
		if !want {
			t.Fatal("joined before the callback returned")
		}
	default:
		if want {
			t.Fatal("not joined after the callback returned")
		}
	}
}

func TestInvalidAndUnsupported(t *testing.T) {
	for _, capacity := range []int{0, -1, int(^uint(0) >> 1)} {
		if _, err := New(capacity); !errors.Is(err, ErrArgument) {
			t.Fatalf("New(%d): %v", capacity, err)
		}
	}
	if _, err := NewEvent(nil); !errors.Is(err, ErrArgument) {
		t.Fatal(err)
	}
	q := queue(t, 1)
	var empty Queue
	var noQueue *Queue
	for _, bad := range []*Queue{&empty, noQueue} {
		if err := bad.Put(&Event{}); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
		if _, err := bad.Remove(&Event{}); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
		if _, err := bad.Dispatch(0); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
		if err := bad.Close(); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
		if err := bad.Error(); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
	}
	var noEvent *Event
	for _, bad := range []*Event{nil, {}} {
		if err := q.Put(bad); !errors.Is(err, ErrArgument) {
			t.Fatal(err)
		}
	}
	if err := noEvent.Release(); !errors.Is(err, ErrArgument) {
		t.Fatal(err)
	}
	if err := (&Event{}).Release(); !errors.Is(err, ErrArgument) {
		t.Fatal(err)
	}
	ev := event(t, func() error { t.Fatal("unsupported wait dispatched"); return nil })
	mustPut(t, q, ev)
	for _, timeout := range []time.Duration{-1, 1, time.Second} {
		if ran, err := q.Dispatch(timeout); ran || !errors.Is(err, ErrUnsupported) {
			t.Fatalf("timeout %s: %t, %v", timeout, ran, err)
		}
	}
	if err := ev.Release(); !errors.Is(err, ErrBusy) {
		t.Fatalf("unsupported wait lost pending ownership: %v", err)
	}
}

func TestFIFOFullDuplicateWrapAndRemove(t *testing.T) {
	q := queue(t, 3)
	var got []int
	var events []*Event
	for i := 0; i < 7; i++ {
		events = append(events, event(t, func() error { got = append(got, i); return nil }))
	}
	mustPut(t, q, events[0])
	mustPut(t, q, events[1])
	mustPut(t, q, events[2])
	mustPut(t, q, events[0])
	if err := q.Put(events[3]); !errors.Is(err, ErrFull) {
		t.Fatalf("full = %v", err)
	}
	if err := events[3].Release(); err != nil {
		t.Fatalf("failed insertion retained ownership: %v", err)
	}
	mustDispatch(t, q)
	mustPut(t, q, events[4])
	if removed, err := q.Remove(events[2]); !removed || err != nil {
		t.Fatalf("remove wrapped middle = %t, %v", removed, err)
	}
	mustPut(t, q, events[5])
	mustDispatch(t, q)
	mustDispatch(t, q)
	mustDispatch(t, q)
	if ran, err := q.Dispatch(0); ran || err != nil {
		t.Fatalf("empty = %t, %v", ran, err)
	}
	if !reflect.DeepEqual(got, []int{0, 1, 4, 5}) {
		t.Fatalf("FIFO = %v", got)
	}
	for _, ev := range []*Event{events[0], events[1], events[2], events[4], events[5]} {
		if err := ev.Release(); err != nil {
			t.Fatal(err)
		}
	}
	for _, position := range []int{0, 1, 2} {
		fresh := queue(t, 3)
		for i := 0; i < 3; i++ {
			mustPut(t, fresh, event(t, func() error { return nil }))
		}
		ev := fresh.events[position]
		if removed, err := fresh.Remove(ev); !removed || err != nil {
			t.Fatalf("remove position %d: %t, %v", position, removed, err)
		}
		if removed, err := fresh.Remove(ev); removed || err != nil {
			t.Fatalf("remove twice: %t, %v", removed, err)
		}
		if err := ev.Release(); err != nil {
			t.Fatal(err)
		}
		mustDispatch(t, fresh)
		mustDispatch(t, fresh)
	}
}

func TestPendingAndRunningOwnership(t *testing.T) {
	q, other := queue(t, 2), queue(t, 2)
	entered, resume := make(chan struct{}), make(chan struct{})
	ev := event(t, func() error {
		close(entered)
		<-resume
		return nil
	})
	mustPut(t, q, ev)
	if err := other.Put(ev); !errors.Is(err, ErrBusy) {
		t.Fatalf("pending stolen: %v", err)
	}
	if removed, err := other.Remove(ev); removed || err != nil {
		t.Fatalf("foreign removal: %t, %v", removed, err)
	}
	if err := ev.Release(); !errors.Is(err, ErrBusy) {
		t.Fatalf("pending released: %v", err)
	}
	returned := make(chan error, 1)
	go func() { _, err := q.Dispatch(0); returned <- err }()
	<-entered
	if err := other.Put(ev); !errors.Is(err, ErrBusy) {
		t.Fatalf("running stolen: %v", err)
	}
	if err := ev.Release(); !errors.Is(err, ErrBusy) {
		t.Fatalf("running released: %v", err)
	}
	if removed, err := q.Remove(ev); removed || err != nil {
		t.Fatalf("running removed: %t, %v", removed, err)
	}
	if ran, err := q.Dispatch(0); ran || !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel dispatcher admitted: %t, %v", ran, err)
	}
	close(resume)
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
	if err := q.Put(ev); !errors.Is(err, ErrClosed) {
		t.Fatalf("released event accepted: %v", err)
	}
}

func TestSelfRequeueAndCloseFromCallback(t *testing.T) {
	q := queue(t, 1)
	var ev *Event
	calls := 0
	ev = event(t, func() error {
		calls++
		mustPut(t, q, ev)
		mustPut(t, q, ev)
		if err := ev.Release(); !errors.Is(err, ErrBusy) {
			t.Fatalf("self release = %v", err)
		}
		if calls == 2 {
			if err := q.Close(); !errors.Is(err, ErrBusy) {
				t.Fatalf("close executing callback = %v", err)
			}
			requireDone(t, q, false)
		}
		return nil
	})
	mustPut(t, q, ev)
	mustDispatch(t, q)
	mustDispatch(t, q)
	requireDone(t, q, true)
	if calls != 2 {
		t.Fatalf("callback count = %d", calls)
	}
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("repeat close = %v", err)
	}
	if ran, err := q.Dispatch(0); ran || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed dispatched = %t, %v", ran, err)
	}
}

func TestCloseJoinsActualReturnAndRetainsLateError(t *testing.T) {
	q := queue(t, 2)
	first, late := errors.New("first callback"), errors.New("late callback")
	ev := event(t, func() error { return first })
	mustPut(t, q, ev)
	if ran, err := q.Dispatch(0); !ran || err != first || q.Error() != first {
		t.Fatalf("first = %t, %v, %v", ran, err, q.Error())
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	active := event(t, func() error { close(entered); <-resume; return late })
	pending := event(t, func() error { t.Fatal("cancelled callback ran"); return nil })
	mustPut(t, q, active)
	returned := make(chan error, 1)
	go func() { _, err := q.Dispatch(0); returned <- err }()
	<-entered
	mustPut(t, q, pending)
	if err := q.Close(); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	requireDone(t, q, false)
	if err := active.Release(); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := pending.Release(); err != nil {
		t.Fatalf("cancelled pending owner retained: %v", err)
	}
	if err := q.Put(ev); !errors.Is(err, ErrClosed) {
		t.Fatalf("closing admission = %v", err)
	}
	close(resume)
	if err := <-returned; err != late {
		t.Fatalf("late actual result lost: %v", err)
	}
	requireDone(t, q, true)
	if q.Error() != first {
		t.Fatalf("first failure overwritten: %v", q.Error())
	}
	if err := active.Release(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("failure falsely prevented resource join: %v", err)
	}
}

func TestFirstFailureCanArriveAfterClose(t *testing.T) {
	q := queue(t, 1)
	cause := errors.New("only late failure")
	ev := event(t, func() error {
		if err := q.Close(); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		if q.Error() != nil {
			t.Fatal("error fabricated before callback return")
		}
		return cause
	})
	mustPut(t, q, ev)
	if ran, err := q.Dispatch(0); !ran || err != cause {
		t.Fatalf("dispatch = %t, %v", ran, err)
	}
	requireDone(t, q, true)
	if q.Error() != cause {
		t.Fatalf("late failure = %v", q.Error())
	}
}

func TestPanicPropagatesButReleasesOwnership(t *testing.T) {
	q := queue(t, 1)
	sentinel := &struct{ n int }{7}
	ev := event(t, func() error {
		if err := q.Close(); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		panic(sentinel)
	})
	mustPut(t, q, ev)
	var got any
	func() {
		defer func() { got = recover() }()
		_, _ = q.Dispatch(0)
	}()
	if got != sentinel {
		t.Fatalf("panic swallowed/replaced: %v", got)
	}
	requireDone(t, q, true)
	if q.Error() != ErrAborted {
		t.Fatalf("abnormal completion = %v", q.Error())
	}
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestGoexitReleasesWithoutPretendingNormalReturn(t *testing.T) {
	q := queue(t, 1)
	ev := event(t, func() error {
		if err := q.Close(); !errors.Is(err, ErrBusy) {
			t.Error(err)
		}
		runtime.Goexit()
		return nil
	})
	mustPut(t, q, ev)
	exited := make(chan struct{})
	go func() { defer close(exited); _, _ = q.Dispatch(0) }()
	<-exited
	requireDone(t, q, true)
	if q.Error() != ErrAborted {
		t.Fatalf("Goexit reported success: %v", q.Error())
	}
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateProducers(t *testing.T) {
	q := queue(t, 1)
	calls := 0
	ev := event(t, func() error { calls++; return nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if err := q.Put(ev); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	mustDispatch(t, q)
	if ran, err := q.Dispatch(0); ran || err != nil || calls != 1 {
		t.Fatalf("duplicate dispatch: %t, %v, calls=%d", ran, err, calls)
	}
}

func TestHandoffDoesNotAllocate(t *testing.T) {
	q := queue(t, 1)
	ev := event(t, func() error { return nil })
	if allocations := testing.AllocsPerRun(100, func() {
		if err := q.Put(ev); err != nil {
			panic(err)
		}
		if removed, err := q.Remove(ev); !removed || err != nil {
			panic("remove failed")
		}
		if err := q.Put(ev); err != nil {
			panic(err)
		}
		if ran, err := q.Dispatch(0); !ran || err != nil {
			panic("dispatch failed")
		}
	}); allocations != 0 {
		t.Fatalf("handoff allocations = %g", allocations)
	}
}
