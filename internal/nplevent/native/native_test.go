//go:build !tinygo && c6_event_hostfixture

package native

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
)

func argument(status int32, block bool) []byte {
	out := make([]byte, 16)
	binary.LittleEndian.PutUint32(out, 0x6436c0de)
	binary.LittleEndian.PutUint32(out[4:], uint32(status))
	if block {
		binary.LittleEndian.PutUint32(out[12:], 1)
	}
	return out
}

func makeQueue(t *testing.T) *nplevent.Queue {
	t.Helper()
	q, err := nplevent.New(2)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func makeNative(t *testing.T, status int32, block bool) *Event {
	t.Helper()
	ev, err := New(fixtureCallback(), argument(status, block))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func put(t *testing.T, ev *Event, q *nplevent.Queue) {
	t.Helper()
	if err := ev.Put(q); err != nil {
		t.Fatal(err)
	}
}

func release(t *testing.T, ev *Event) {
	t.Helper()
	if err := ev.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateNativeBridge(t *testing.T) {
	t.Run("actual-C-copy-status-and-repeat", func(t *testing.T) {
		fixtureReset()
		abi, err := ABI()
		if err != nil || abi[0] != 1 || abi[1] != 8 || abi[2] != 4 ||
			abi[4] != 32 || abi[5] != 8 || abi[6] == 0 || abi[7] != 8 {
			t.Fatalf("actual HOST ABI = %v, %v", abi, err)
		}
		q := makeQueue(t)
		input := argument(-17, false)
		ev, err := New(fixtureCallback(), input)
		if err != nil {
			t.Fatal(err)
		}
		for i := range input {
			input[i] = 0
		}
		put(t, ev, q)
		put(t, ev, q)
		if ran, err := q.Dispatch(0); !ran || err != (CallbackError{-17}) {
			t.Fatalf("real C callback/copy result = %t, %v", ran, err)
		}
		if status, err := ev.FirstFailure(); status != -17 || err != nil {
			t.Fatalf("native first status = %d, %v", status, err)
		}
		put(t, ev, q)
		if ran, err := q.Dispatch(0); !ran || err != nil || fixtureReturns() != 2 {
			t.Fatalf("C-owned mutable argument/repeat = %t, %v, %d", ran, err, fixtureReturns())
		}
		if q.Error() != (CallbackError{-17}) {
			t.Fatalf("first queue cause lost: %v", q.Error())
		}
		release(t, ev)
		if err := ev.Release(); !errors.Is(err, ErrStale) {
			t.Fatalf("second native release = %v", err)
		}
	})
	t.Run("queued-cancel-and-release", func(t *testing.T) {
		fixtureReset()
		q := makeQueue(t)
		ev := makeNative(t, 0, false)
		put(t, ev, q)
		if err := ev.Release(); !errors.Is(err, nplevent.ErrBusy) {
			t.Fatal(err)
		}
		if removed, err := ev.Remove(q); !removed || err != nil {
			t.Fatalf("cancel = %t,%v", removed, err)
		}
		release(t, ev)
		if ran, err := q.Dispatch(0); ran || err != nil || fixtureReturns() != 0 {
			t.Fatalf("cancelled callback = %t,%v,%d", ran, err, fixtureReturns())
		}
		ev = makeNative(t, 0, false)
		put(t, ev, q)
		if err := q.Close(); err != nil {
			t.Fatal(err)
		}
		release(t, ev)
	})
	t.Run("C-return-owns-release-and-late-error", func(t *testing.T) {
		fixtureReset()
		q := makeQueue(t)
		ev := makeNative(t, -2147483648, true)
		handle := fixtureSnapshot(ev)
		put(t, ev, q)
		returned := make(chan error, 1)
		exited := make(chan struct{})
		go func() { defer close(exited); _, err := q.Dispatch(0); returned <- err }()
		t.Cleanup(func() { fixtureResume(); <-exited })
		fixtureWaitEntered()
		if err := q.Close(); !errors.Is(err, nplevent.ErrBusy) {
			t.Fatal(err)
		}
		select {
		case <-q.Done():
			t.Fatal("joined before native C returned")
		default:
		}
		if fixtureReturns() != 0 || q.Error() != nil {
			t.Fatal("native return/error fabricated")
		}
		if err := ev.Release(); !errors.Is(err, nplevent.ErrBusy) {
			t.Fatal(err)
		}
		if err := fixtureRelease(handle); !errors.Is(err, ErrBusy) {
			t.Fatalf("C slot reclaimed while running: %v", err)
		}
		if _, err := fixtureFinish(handle); !errors.Is(err, ErrUnreturned) {
			t.Fatalf("C finish forged return: %v", err)
		}
		if _, err := ev.FirstFailure(); !errors.Is(err, ErrBusy) {
			t.Fatalf("in-flight C status read = %v", err)
		}
		fixtureResume()
		if err := <-returned; err != (CallbackError{-2147483648}) {
			t.Fatalf("late signed status lost: %v", err)
		}
		<-q.Done()
		if fixtureReturns() != 1 || q.Error() != (CallbackError{-2147483648}) {
			t.Fatalf("late ownership/cause = %d,%v", fixtureReturns(), q.Error())
		}
		if status, err := ev.FirstFailure(); status != -2147483648 || err != nil {
			t.Fatalf("native late first = %d,%v", status, err)
		}
		release(t, ev)
	})
	t.Run("stale-handles-cannot-alias-reuse", func(t *testing.T) {
		fixtureReset()
		ev := makeNative(t, 0, false)
		old := fixtureSnapshot(ev)
		release(t, ev)
		current := makeNative(t, 0, false)
		now := fixtureSnapshot(current)
		if fixtureIndex(old) != fixtureIndex(now) || fixtureGeneration(old) == fixtureGeneration(now) {
			t.Fatalf("reuse/generation not exercised: %v,%v", old, now)
		}
		for _, err := range []error{fixtureClaim(old), fixtureInvoke(old), fixtureRelease(old)} {
			if !errors.Is(err, ErrStale) {
				t.Fatalf("old handle accepted: %v", err)
			}
		}
		if _, err := fixtureFinish(old); !errors.Is(err, ErrStale) {
			t.Fatal(err)
		}
		if _, err := ev.FirstFailure(); !errors.Is(err, ErrStale) {
			t.Fatal(err)
		}
		q := makeQueue(t)
		put(t, current, q)
		if ran, err := q.Dispatch(0); !ran || err != nil || fixtureReturns() != 1 {
			t.Fatalf("current generation damaged: %t,%v", ran, err)
		}
		release(t, current)
	})
	t.Run("exhaustion-and-constructor-cleanup", func(t *testing.T) {
		for i := 0; i < 16; i++ {
			if _, err := New(0, nil); !errors.Is(err, ErrArgument) {
				t.Fatal(err)
			}
			if _, err := New(fixtureCallback(), make([]byte, 33)); !errors.Is(err, ErrArgument) {
				t.Fatal(err)
			}
		}
		var owners []*Event
		for i := 0; i < 8; i++ {
			owners = append(owners, makeNative(t, 0, false))
		}
		for i := 0; i < 16; i++ {
			if _, err := New(fixtureCallback(), argument(0, false)); !errors.Is(err, ErrExhausted) {
				t.Fatalf("exhausted = %v", err)
			}
		}
		for _, ev := range owners {
			release(t, ev)
		}
		ev := makeNative(t, 0, false)
		release(t, ev)
	})
	t.Run("claim-cannot-finish-without-C-return", func(t *testing.T) {
		fixtureReset()
		ev := makeNative(t, 9, false)
		h := fixtureSnapshot(ev)
		if err := fixtureClaim(h); err != nil {
			t.Fatal(err)
		}
		if err := fixtureClaim(h); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		if _, err := fixtureFinish(h); !errors.Is(err, ErrUnreturned) {
			t.Fatal(err)
		}
		if err := fixtureRelease(h); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		if err := fixtureInvoke(h); err != nil {
			t.Fatal(err)
		}
		if status, err := fixtureFinish(h); status != 9 || err != nil {
			t.Fatalf("confirmed C return = %d,%v", status, err)
		}
		release(t, ev)
	})
	t.Run("generation-wrap-retires-instead-of-aliasing", func(t *testing.T) {
		ev := makeNative(t, 0, false)
		old := fixtureSnapshot(ev)
		release(t, ev)
		if err := fixtureNearWrap(old); err != nil {
			t.Fatal(err)
		}
		last := makeNative(t, 0, false)
		max := fixtureSnapshot(last)
		if fixtureIndex(max) != fixtureIndex(old) || fixtureGeneration(max) != ^uint32(0) {
			t.Fatal("real max-generation arm not reached")
		}
		release(t, last)
		next := makeNative(t, 0, false)
		if fixtureIndex(fixtureSnapshot(next)) == fixtureIndex(max) {
			t.Fatal("generation wrapped and reused retired slot")
		}
		if err := fixtureClaim(max); !errors.Is(err, ErrStale) {
			t.Fatal(err)
		}
		release(t, next)
	})
}
