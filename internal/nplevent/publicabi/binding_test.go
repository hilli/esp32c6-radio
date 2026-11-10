//go:build !tinygo && c6_public_hostfixture

package publicabi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
)

func TestPublicVoidMisuseFaultChild(t *testing.T) {
	if os.Getenv("C6_PUBLIC_FAULT_CHILD") != "1" {
		t.Skip("explicit native fault subprocess only")
	}
	fixtureArmFaultExit()
	p := nativeOwner(t, 0)
	b, q := bindOwner(t, p), newQueue(t)
	if err := b.Put(q); err != nil {
		t.Fatal(err)
	}
	fixturePublicCycle(p) // public void init of QUEUED event must fault, not lie
	t.Fatal("public misuse returned instead of fail-stop")
}

func TestPublicVoidMisuseActuallyFaults(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "-test.run=^TestPublicVoidMisuseFaultChild$")
	cmd.Env = append(os.Environ(), "C6_PUBLIC_FAULT_CHILD=1")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 75 {
		t.Fatalf("real public trap/fault code: %v, %s", err, output)
	}
}

func nativeOwner(t *testing.T, mode uint32) unsafe.Pointer {
	t.Helper()
	p, err := newFixture(mode)
	if err != nil || p == nil {
		t.Fatalf("native fixture create: %v", err)
	}
	return p
}
func bindOwner(t *testing.T, p unsafe.Pointer) *Binding {
	t.Helper()
	b, err := Bind(fixtureEvent(p))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func newQueue(t *testing.T) *nplevent.Queue {
	t.Helper()
	q, err := nplevent.New(1)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func finishOwner(t *testing.T, p unsafe.Pointer, b *Binding) {
	t.Helper()
	if b != nil {
		if err := b.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if err := destroyFixture(p); err != nil {
		t.Fatal(err)
	}
}
func TestPublicEventAdapter(t *testing.T) {
	t.Run("actual-public-signatures-and-borrowed-pointer-identity", func(t *testing.T) {
		p := nativeOwner(t, 0)
		fixturePublicCycle(p)
		if fixtureCalls(p) != 1 || fixtureIssue(p) != 0 {
			t.Fatalf("real void callback identity: %d,%d", fixtureCalls(p), fixtureIssue(p))
		}
		finishOwner(t, p, nil)
	})
	t.Run("real-queued-state-cancel-close-and-full-rollback", func(t *testing.T) {
		p := nativeOwner(t, 0)
		b, q := bindOwner(t, p), newQueue(t)
		if fixtureQueued(p) {
			t.Fatal("idle marked queued")
		}
		if err := b.Put(q); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(q); err != nil || !fixtureQueued(p) {
			t.Fatalf("duplicate/reservation: %v", err)
		}
		if err := fixtureTry(p, 0); err != (Error{11}) {
			t.Fatalf("queued deinit accepted: %v", err)
		}
		if err := b.Release(); err != nplevent.ErrBusy {
			t.Fatal(err)
		}
		if removed, err := b.Remove(q); !removed || err != nil || fixtureQueued(p) {
			t.Fatalf("cancel/native state: %t,%v", removed, err)
		}
		filler, _ := nplevent.NewEvent(func() error { return nil })
		if err := q.Put(filler); err != nil {
			t.Fatal(err)
		}
		if err := b.Put(q); !errors.Is(err, nplevent.ErrFull) || fixtureQueued(p) {
			t.Fatalf("failed admission/native rollback = %v", err)
		}
		if removed, err := q.Remove(filler); !removed || err != nil {
			t.Fatal(err)
		}
		if err := b.Put(q); err != nil {
			t.Fatal(err)
		}
		if err := q.Close(); err != nil || fixtureQueued(p) {
			t.Fatalf("queue Close must cancel native reservation: %v", err)
		}
		finishOwner(t, p, b)
	})
	t.Run("foreign-thread-retirement-rejected-until-real-return", func(t *testing.T) {
		p := nativeOwner(t, 1)
		b, q := bindOwner(t, p), newQueue(t)
		if err := b.Put(q); err != nil {
			t.Fatal(err)
		}
		returned, exited := make(chan error, 1), make(chan struct{})
		go func() { defer close(exited); _, err := q.Dispatch(0); returned <- err }()
		t.Cleanup(func() { fixtureResume(); <-exited })
		fixtureWait()
		for op := uint32(0); op < 5; op++ {
			if err := fixtureTry(p, op); err != (Error{11}) {
				t.Fatalf("active foreign operation %d = %v", op, err)
			}
		}
		if err := destroyFixture(p); err != (Error{11}) || fixtureLive() != 1 {
			t.Fatalf("caller owner freed in flight: %v", err)
		}
		if err := b.Release(); err != nplevent.ErrBusy {
			t.Fatal(err)
		}
		if err := q.Close(); err != nplevent.ErrBusy {
			t.Fatal(err)
		}
		select {
		case <-q.Done():
			t.Fatal("joined before C void callback return")
		default:
		}
		fixtureResume()
		if err := <-returned; err != nil {
			t.Fatal(err)
		}
		<-q.Done()
		if fixtureCalls(p) != 1 || fixtureIssue(p) != 0 {
			t.Fatal("active callback data lost")
		}
		finishOwner(t, p, b)
	})
	t.Run("actual-self-deinit-protected-wrapper-and-caller-arg-free", func(t *testing.T) {
		p := nativeOwner(t, 2)
		b, q := bindOwner(t, p), newQueue(t)
		if err := b.Put(q); err != nil {
			t.Fatal(err)
		}
		if ran, err := q.Dispatch(0); !ran || err != nil {
			t.Fatalf("actual void self-retirement = %t,%v", ran, err)
		}
		if fixtureCalls(p) != 1 || fixtureIssue(p) != 0 {
			t.Fatal("protected wrapper/caller pointee retirement failed")
		}
		if err := b.Put(q); err != (Error{10}) {
			t.Fatalf("retired generation scheduled: %v", err)
		}
		finishOwner(t, p, b) // must NOT dereference the PROT_NONE public wrapper
	})
	t.Run("same-address-reinit-before-old-callback-return", func(t *testing.T) {
		p := nativeOwner(t, 3)
		old, q := bindOwner(t, p), newQueue(t)
		if err := old.Put(q); err != nil {
			t.Fatal(err)
		}
		if ran, err := q.Dispatch(0); !ran || err != nil {
			t.Fatal(err)
		}
		if err := old.Put(q); err != (Error{10}) {
			t.Fatalf("old generation reused new caller object: %v", err)
		}
		if err := old.Release(); err != nil {
			t.Fatal(err)
		}
		current := bindOwner(t, p)
		if err := current.Put(q); err != nil {
			t.Fatal(err)
		}
		if ran, err := q.Dispatch(0); !ran || err != nil || fixtureCalls(p) != 2 || fixtureIssue(p) != 0 {
			t.Fatalf("new generation damaged by old finish: %t,%v", ran, err)
		}
		finishOwner(t, p, current)
	})
	t.Run("quiescent-reinit-and-wrapper-copy-stale-fencing", func(t *testing.T) {
		p := nativeOwner(t, 0)
		b := bindOwner(t, p)
		fixtureReinit(p)
		if err := b.Put(newQueue(t)); err != (Error{10}) {
			t.Fatalf("stale bound generation = %v", err)
		}
		if err := fixtureTry(p, 5); err != (Error{10}) {
			t.Fatalf("copied public wrapper accepted: %v", err)
		}
		finishOwner(t, p, b)
	})
	t.Run("native-owner-exhaustion-cleanup", func(t *testing.T) {
		var owners []unsafe.Pointer
		for i := 0; i < 8; i++ {
			owners = append(owners, nativeOwner(t, 0))
		}
		for i := 0; i < 8; i++ {
			if p, err := newFixture(0); p != nil || err != (Error{1}) || fixtureLive() != 8 {
				t.Fatalf("native failure cleanup = %p,%v,%d", p, err, fixtureLive())
			}
		}
		for _, p := range owners {
			finishOwner(t, p, nil)
		}
	})
	if fixtureLive() != 0 {
		t.Fatalf("caller owner leak = %d", fixtureLive())
	}
}
