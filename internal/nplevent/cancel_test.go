package nplevent

import (
	"errors"
	"testing"
)

func TestCancelHookRetainsRejectedOwnership(t *testing.T) {
	cause := errors.New("native cancellation held")
	fail, calls := true, 0
	ev, err := NewEventWithCancel(func() error { t.Fatal("closed callback ran"); return nil }, func() error {
		calls++
		if fail {
			return cause
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	q := queue(t, 1)
	mustPut(t, q, ev)
	if removed, err := q.Remove(ev); removed || err != cause {
		t.Fatalf("rejected removal = %t,%v", removed, err)
	}
	if err := ev.Release(); err != ErrBusy {
		t.Fatalf("rejected cancellation released owner: %v", err)
	}
	if err := q.Close(); err != cause {
		t.Fatal(err)
	}
	requireDone(t, q, false)
	if q.Error() != cause {
		t.Fatalf("cancellation failure lost: %v", q.Error())
	}
	fail = false
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	requireDone(t, q, true)
	if calls != 3 || q.Error() != cause {
		t.Fatalf("retry/history = %d,%v", calls, q.Error())
	}
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
}
