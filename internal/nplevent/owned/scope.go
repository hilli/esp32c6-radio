//go:build c6_owned_entry

// Package owned is an opt-in scoped Go/native entry service, not a native
// scheduler, task-join API or controller port.
package owned

/*
#cgo !tinygo CFLAGS: -pthread
#cgo !tinygo LDFLAGS: -pthread
#include "internal.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"strconv"
	"sync/atomic"
)

type Error uint32

func (err Error) Error() string {
	return "owned C/Go entry control " + strconv.FormatUint(uint64(err), 10)
}

const (
	ErrUnowned   Error = C.C6_OWNED_UNOWNED
	ErrTrap      Error = C.C6_OWNED_TRAP
	ErrMasked    Error = C.C6_OWNED_MASKED
	ErrExhausted Error = C.C6_OWNED_EXHAUSTED
	ErrBusy      Error = C.C6_OWNED_BUSY
	ErrPanic     Error = C.C6_OWNED_PANIC
	ErrInvalid   Error = C.C6_OWNED_INVALID
)

type scope struct {
	body       func() error
	callback   func(uint32) (int32, error)
	first      error
	panicked   bool
	panicValue any
}

var exportEntries atomic.Uint32

// ExportCount is a diagnostic: refused native calls must not increment it.
func ExportCount() uint32 { return exportEntries.Load() }

// With retains Go closures and runtime identity until the actual native scope
// returns. Calls are foreground only. Goexit/nonlocal C unwind and foreign task
// stacks are unsupported; this is never forced cancellation or task join.
func With(body func() error, callback func(uint32) (int32, error)) error {
	if body == nil || callback == nil {
		return ErrInvalid
	}
	if rc := C.c6_owned_admission(); rc != 0 {
		return Error(rc)
	}
	s := &scope{body: body, callback: callback}
	rc, err := platformRun(s)
	runtime.KeepAlive(s)
	if s.panicked {
		panic(s.panicValue)
	}
	if rc != 0 {
		return errors.Join(s.first, Error(rc), err)
	}
	if err != nil {
		return errors.Join(s.first, err)
	}
	return s.first
}

// Identity is diagnostic, runtime-derived, and valid only inside With. It is
// not a token an unowned caller can supply to gain entry.
func Identity() uintptr { return uintptr(C.c6_owned_identity()) }

// Call is the guarded typed C-to-Go path, also callable from a genuine C
// callback. Application status and port control errors are separate.
func Call(request uint32) (int32, error) {
	var value C.int32_t
	rc := C.c6_owned_call_go(C.uint32_t(request), &value)
	if rc != 0 {
		return int32(value), Error(rc)
	}
	return int32(value), nil
}

//export c6_owned_go_export
func c6_owned_go_export(request C.uint32_t, out *C.int32_t) (result C.uint32_t) {
	exportEntries.Add(1)
	s := platformCurrent()
	if s == nil || out == nil {
		return C.C6_OWNED_UNOWNED
	}
	// A Go panic must not unwind through native C frames. Preserve it and
	// re-panic only after all native frames and the scope have actually returned.
	completed := false
	defer func() {
		if !completed {
			value := recover()
			if !s.panicked {
				s.panicked, s.panicValue = true, value
			}
			result = C.C6_OWNED_PANIC
		}
	}()
	var err error
	if request == 0 {
		err = s.body()
	} else {
		var value int32
		value, err = s.callback(uint32(request))
		*out = C.int32_t(value)
	}
	if err != nil && s.first == nil {
		s.first = err
	}
	completed = true
	return C.C6_OWNED_OK
}
