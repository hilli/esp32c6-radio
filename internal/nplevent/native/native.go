// Package native supplies a PRIVATE C callback/owned-argument bridge for
// nplevent. It is not the vendor NPL ABI, a native task port or a BLE backend.
package native

/*
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"strconv"
	"unsafe"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
)

var (
	ErrArgument   = errors.New("native event: invalid private ABI argument")
	ErrStale      = errors.New("native event: stale generation handle")
	ErrBusy       = errors.New("native event: native callback still owned")
	ErrExhausted  = errors.New("native event: fixed slots exhausted")
	ErrUnreturned = errors.New("native event: C callback return not confirmed")
)

// Callback is the address of a statically linked C function with the exact
// bridge.h private signature, never a Go function value or vendor NPL callback.
type Callback uintptr

// Event must not be copied. Its C slot owns at most 32 copied argument bytes;
// no Go pointer or caller argument buffer remains borrowed after New returns.
type Event struct {
	noCopy noCopy
	handle C.struct_c6_event_handle
	event  *nplevent.Event
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type CallbackError struct{ Status int32 }
type BridgeError struct{ Status int32 }

func (err CallbackError) Error() string {
	return "native event callback status " + strconv.FormatInt(int64(err.Status), 10)
}

func (err BridgeError) Error() string {
	return "native event bridge status " + strconv.FormatInt(int64(err.Status), 10)
}

func bridgeError(status C.int32_t) error {
	switch status {
	case C.C6_EVENT_OK:
		return nil
	case C.C6_EVENT_INVALID:
		return ErrArgument
	case C.C6_EVENT_STALE:
		return ErrStale
	case C.C6_EVENT_BUSY:
		return ErrBusy
	case C.C6_EVENT_EXHAUSTED:
		return ErrExhausted
	case C.C6_EVENT_UNRETURNED:
		return ErrUnreturned
	default:
		return BridgeError{Status: int32(status)}
	}
}

func New(callback Callback, argument []byte) (*Event, error) {
	if inISR() {
		return nil, nplevent.ErrISR
	}
	if callback == 0 || len(argument) > C.C6_EVENT_ARGUMENT_BYTES {
		return nil, ErrArgument
	}
	owner := &Event{}
	var err error
	owner.event, err = nplevent.NewEvent(owner.dispatch)
	if err != nil {
		return nil, err
	}
	var data unsafe.Pointer
	if len(argument) != 0 {
		data = unsafe.Pointer(&argument[0])
	}
	key := lock()
	result := C.c6_event_create(C.uintptr_t(callback), data, C.uint32_t(len(argument)), &owner.handle)
	unlock(key)
	runtime.KeepAlive(argument)
	if err := bridgeError(result); err != nil {
		return nil, errors.Join(err, owner.event.Release())
	}
	return owner, nil
}

func (owner *Event) dispatch() error {
	key := lock()
	result := C.c6_event_claim(&owner.handle)
	unlock(key)
	if err := bridgeError(result); err != nil {
		return err
	}
	// No service critical section is held across the actual C function pointer
	// call. C records returned only AFTER that function really returns.
	if err := bridgeError(C.c6_event_invoke(&owner.handle)); err != nil {
		return err
	}
	var status C.int32_t
	key = lock()
	result = C.c6_event_finish(&owner.handle, &status)
	unlock(key)
	if err := bridgeError(result); err != nil {
		return err
	}
	if status != 0 {
		return CallbackError{Status: int32(status)}
	}
	return nil
}

func (owner *Event) Put(queue *nplevent.Queue) error {
	if owner == nil {
		return ErrArgument
	}
	return queue.Put(owner.event)
}

func (owner *Event) Remove(queue *nplevent.Queue) (bool, error) {
	if owner == nil {
		return false, ErrArgument
	}
	return queue.Remove(owner.event)
}

// Release refuses pending/running Go ownership before touching the native
// slot. A claimed C slot additionally refuses reclamation until return/finish.
// Repeated release returns ErrStale; no generation wraps into a live alias.
func (owner *Event) Release() error {
	if owner == nil {
		return ErrArgument
	}
	if err := owner.event.Release(); err != nil {
		return err
	}
	key := lock()
	result := C.c6_event_release(&owner.handle)
	unlock(key)
	return bridgeError(result)
}

func (owner *Event) FirstFailure() (int32, error) {
	if owner == nil {
		return 0, ErrArgument
	}
	if inISR() {
		return 0, nplevent.ErrISR
	}
	var status C.int32_t
	key := lock()
	result := C.c6_event_first_failure(&owner.handle, &status)
	unlock(key)
	return int32(status), bridgeError(result)
}

// ABI reports version, handle size/alignment, code-pointer size, argument
// capacity/alignment, slot size and slot count from the actual C compiler.
func ABI() ([8]uint32, error) {
	var out [8]uint32
	if inISR() {
		return out, nplevent.ErrISR
	}
	var values [8]C.uint32_t
	C.c6_event_abi_read(&values[0])
	for i, value := range values {
		out[i] = uint32(value)
	}
	return out, nil
}
