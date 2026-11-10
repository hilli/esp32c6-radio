//go:build !tinygo && c6_owned_entry && c6_owned_hostfixture

package owned_test

import (
	"errors"
	"runtime"
	"testing"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
	"github.com/hilli/esp32c6-radio/internal/nplevent/owned"
	"github.com/hilli/esp32c6-radio/internal/nplevent/publicabi"
)

func TestGuardRejectsBeforeGoEntry(t *testing.T) {
	before := owned.ExportCount()
	if _, err := owned.Call(1); err != owned.ErrUnowned {
		t.Fatal(err)
	}
	if owned.ExportCount() != before {
		t.Fatal("unowned native call entered Go before rejecting")
	}
	p := owned.FixtureNew()
	if p == nil {
		t.Fatal("native fixture allocation")
	}
	owned.FixtureRaw(p) // preserved default C-only callback, guarded Go refusal
	if owned.FixtureControl(p) != 1 || owned.ExportCount() != before {
		t.Fatal("unowned C event callback crossed managed Go entry")
	}
	err := owned.With(func() error {
		entries := owned.ExportCount()
		if rc := owned.FixtureForeignCall(); rc != 1 {
			t.Fatalf("foreign pthread admission = %d", rc)
		}
		if owned.ExportCount() != entries {
			t.Fatal("foreign C pthread caused a Go entry/context")
		}
		value, err := owned.Call(2)
		if err != nil || value != 23 {
			t.Fatalf("legal typed return = %d,%v", value, err)
		}
		return nil
	}, func(request uint32) (int32, error) { return int32(request + 21), nil })
	if err != nil {
		t.Fatal(err)
	}
	if owned.Identity() != 0 {
		t.Fatal("owner identity escaped scope")
	}
	if rc := owned.FixtureDestroy(p); rc != 0 {
		t.Fatal(rc)
	}
}

func TestActualGoYieldCompetingOwnersAndNativeRetirement(t *testing.T) {
	p := owned.FixtureNew()
	if p == nil {
		t.Fatal("native fixture allocation")
	}
	entered, permit := make(chan struct{}), make(chan struct{})
	first := errors.New("late owned callback failure")
	aResult := make(chan error, 1)
	var aIdentity, bIdentity uintptr
	go func() {
		aResult <- owned.With(func() error {
			binding, err := publicabi.Bind(owned.FixtureEvent(p))
			if err != nil {
				return err
			}
			q, err := nplevent.New(1)
			if err != nil {
				return err
			}
			if err := binding.Put(q); err != nil {
				return err
			}
			if ran, err := q.Dispatch(0); !ran || err != nil {
				return errors.New("native callback did not return normally")
			}
			if err := binding.Put(q); err != (publicabi.Error{Code: 10}) {
				return errors.New("retired generation became schedulable")
			}
			if err := binding.Release(); err != nil {
				return err
			}
			return q.Close()
		}, func(request uint32) (int32, error) {
			aIdentity = owned.Identity()
			captured := make([]byte, 1024)
			captured[0] = 97
			close(entered)
			runtime.Gosched() // real Go scheduler yield with C frames outstanding
			<-permit
			runtime.GC()
			if owned.Identity() != aIdentity || captured[0] != 97 {
				return 0, errors.New("owner/closure changed across yield")
			}
			if rc := owned.FixtureRetire(p); rc != 0 {
				return 0, errors.New("self retirement lost native owner after yield")
			}
			return -2147483648, first
		})
	}()
	select {
	case <-entered:
	case err := <-aResult:
		t.Fatalf("owned entry failed before yield: %v", err)
	}
	err := owned.With(func() error {
		bIdentity = owned.Identity()
		if bIdentity == 0 || bIdentity == aIdentity {
			return errors.New("competing actual owners conflated")
		}
		if rc := owned.FixtureForeignRetire(p); rc != 11 {
			return errors.New("other Go owner mistaken for native frame self")
		}
		value, err := owned.Call(2)
		if value != 42 || err != nil {
			return errors.New("second owner typed entry/return failed")
		}
		return nil
	}, func(uint32) (int32, error) { runtime.Gosched(); return 42, nil })
	close(permit)
	aErr := <-aResult
	if err != nil || aErr != first {
		t.Fatalf("owner errors/late cause = %v,%v", err, aErr)
	}
	if owned.FixtureReturns(p) != 1 || owned.FixtureControl(p) != 0 ||
		owned.FixtureStatus(p) != -2147483648 {
		t.Fatalf("C-Go-C return/status = %d,%d,%d",
			owned.FixtureReturns(p), owned.FixtureControl(p), owned.FixtureStatus(p))
	}
	if rc := owned.FixtureDestroy(p); rc != 0 {
		t.Fatal(rc)
	}
	before := owned.ExportCount()
	if _, err := owned.Call(1); err != owned.ErrUnowned || owned.ExportCount() != before {
		t.Fatal("closed owner admitted later native call")
	}
}

func TestFirstErrorAndPanicReturnBarrier(t *testing.T) {
	first, late := errors.New("first"), errors.New("late")
	calls := 0
	err := owned.With(func() error {
		_, _ = owned.Call(1)
		_, _ = owned.Call(2)
		return nil
	}, func(uint32) (int32, error) {
		calls++
		if calls == 1 {
			return 5, first
		}
		return 6, late
	})
	if err != first {
		t.Fatalf("first cause replaced after native return: %v", err)
	}
	sentinel := &struct{ n int }{71}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = owned.With(func() error {
			_, err := owned.Call(1)
			if err != owned.ErrPanic {
				t.Fatalf("panic crossed C return uncontrolled: %v", err)
			}
			return nil
		}, func(uint32) (int32, error) { panic(sentinel) })
	}()
	if recovered != sentinel || owned.Identity() != 0 {
		t.Fatal("panic value/actual native scope cleanup lost")
	}
}
