package esp32c6radio

import (
	"errors"
	"testing"
	"time"

	"github.com/hilli/gothread/radio"
)

func TestFlashPauseDoesNotWaitForQueuedCallback(t *testing.T) {
	tasks := make(chan func(), 4)
	cb := &callbacks{}
	d, h := setup(t, cb, func(fn func()) { tasks <- fn })
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	start := <-tasks // Simulate a callback queued on the stack loop running Set.
	finished := make(chan error, 1)
	go func() {
		finished <- d.WithFlashAccess(func() error {
			key := lock()
			defer unlock(key)
			if !d.txEnded || !d.flashPaused || !h.idle() {
				return errors.New("DMA not quiescent")
			}
			return nil
		})
	}()
	time.Sleep(time.Millisecond)
	key := lock()
	d.transmitDone(nil, radio.RxInfo{}, nil)
	unlock(key)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Set waited for callback on blocked stack loop")
	}
	start()
	(<-tasks)()
	next(t, cb, "start")
	next(t, cb, "done")
	if d.GetState() != radio.StateReceive {
		t.Fatal("receive was not restored")
	}
}
func TestFlashPausePreservesModesAndRestoresAfterError(t *testing.T) {
	d, _ := setup(t, &callbacks{}, nil)
	problem := errors.New("synthetic flash error")
	err := d.WithFlashAccess(func() error {
		if d.Transmit(txFrame()) != ErrBusy || d.Sleep() != ErrBusy || d.Disable() != ErrBusy || d.EnergyScan(15, 1) != ErrBusy {
			return errors.New("operation escaped pause")
		}
		return problem
	})
	if err != problem || d.GetState() != radio.StateReceive || d.Statistics().FlashPauses != 1 {
		t.Fatal(err)
	}
}
func TestFlashPauseWaitsForAutomaticACKNotUserCallback(t *testing.T) {
	d, h := macFixture()
	fillRX(h, []byte{0x21, 0x10, 3}, -70, 100)
	h.irq(rxDone)
	if !h.sendingAck {
		t.Fatal("no automatic ACK")
	}
	finished := make(chan error, 1)
	go func() {
		finished <- d.WithFlashAccess(func() error {
			key := lock()
			defer unlock(key)
			if h.sendingAck || !h.idle() {
				return errors.New("automatic ACK/DMA interrupted")
			}
			return nil
		})
	}()
	time.Sleep(time.Millisecond)
	h.irq(ackTXDone)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("automatic ACK pause deadlocked")
	}
	if d.count != 1 {
		t.Fatal("receive callback was consumed/lost during pause")
	}
}
func TestFlashPauseTimeoutDoesNotForceAbortOrMutate(t *testing.T) {
	d, h := setup(t, &callbacks{}, nil)
	key := lock()
	h.blocked = true
	before := h.calls
	unlock(key)
	called := false
	if err := d.WithFlashAccess(func() error { called = true; return nil }); err != ErrBusy {
		t.Fatal(err)
	}
	key = lock()
	after := h.calls
	h.blocked = false
	unlock(key)
	if called || after != before {
		t.Fatal("timed out ACK was aborted or flash mutated")
	}
}
