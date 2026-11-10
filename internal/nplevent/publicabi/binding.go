// Package publicabi adapts the real C void(ble_npl_event*) event ABI. It is not
// a complete NPL/controller port. C owns public wrapper and argument pointees.
package publicabi

/*
#cgo !tinygo CFLAGS: -pthread
#cgo !tinygo LDFLAGS: -pthread
#include "adapter.h"
*/
import "C"

import (
	"errors"
	"strconv"
	"sync"
	"unsafe"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
)

// Error is an event-port control failure, never a return value from the real
// void callback. Its codes are from the retained generic NPL declaration.
type Error struct{ Code int32 }

func (err Error) Error() string {
	return "public NPL event control " + strconv.FormatInt(int64(err.Code), 10)
}

// Cancellation runs under the common queue's critical section. Retained
// enum errors are preboxed so failure reporting there does not allocate.
var controlErrors = [...]error{
	nil, Error{1}, Error{2}, Error{3}, Error{4}, Error{5}, Error{6},
	Error{7}, Error{8}, Error{9}, Error{10}, Error{11}, Error{12},
}
var errInvalidControl = errors.New("public NPL event: control code outside retained enumeration")

func control(rc C.ble_npl_error_t) error {
	if uint64(rc) >= uint64(len(controlErrors)) {
		return errInvalidControl
	}
	return controlErrors[uint64(rc)]
}

// Binding must not be copied. It retains a generation reference to PORT state,
// not the caller's wrapper or arg pointee. Native callers must use successful
// event deinit/retirement and their own borrower lifetime before freeing data.
type Binding struct {
	mu       sync.Mutex
	ref      C.struct_c6_public_ref
	event    *nplevent.Event
	released bool
}

// Bind requires a live native C public event initialized by this port. Never
// pass Go storage/pointers, a FreeRTOS private object, or an expired raw pointer.
// Only an existing foreground Go task may enter this Go-to-C queue adapter.
func Bind(nativeEvent unsafe.Pointer) (*Binding, error) {
	if inISR() {
		return nil, nplevent.ErrISR
	}
	b := &Binding{}
	var err error
	b.event, err = nplevent.NewEventWithCancel(b.dispatch, b.cancel)
	if err != nil {
		return nil, err
	}
	if err := control(C.c6_public_bind((*C.struct_ble_npl_event)(nativeEvent), &b.ref)); err != nil {
		return nil, errors.Join(err, b.event.Release())
	}
	return b, nil
}

func (b *Binding) Put(q *nplevent.Queue) error {
	if b == nil {
		return controlErrors[C.BLE_NPL_EINVAL]
	}
	if inISR() {
		return nplevent.ErrISR
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return controlErrors[C.BLE_NPL_ENOENT]
	}
	var added C.uint32_t
	if err := control(C.c6_public_reserve(&b.ref, &added)); err != nil {
		return err
	}
	if err := q.Put(b.event); err != nil {
		if added != 0 {
			return errors.Join(err, control(C.c6_public_cancel(&b.ref)))
		}
		return err
	}
	return nil
}

func (b *Binding) Remove(q *nplevent.Queue) (bool, error) {
	if b == nil {
		return false, controlErrors[C.BLE_NPL_EINVAL]
	}
	if inISR() {
		return false, nplevent.ErrISR
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	removed, err := q.Remove(b.event)
	if err != nil || !removed {
		return removed, err
	}
	return true, nil
}

func (b *Binding) cancel() error { return control(C.c6_public_cancel(&b.ref)) }

func (b *Binding) dispatch() error {
	b.mu.Lock()
	err := control(C.c6_public_claim(&b.ref))
	b.mu.Unlock()
	if err != nil {
		return err
	}
	return control(C.c6_public_invoke(&b.ref))
}

// Release only releases this binding, never caller wrapper/argument data.
// Cancel pending work first. Running callbacks retain ownership until return.
func (b *Binding) Release() error {
	if b == nil {
		return controlErrors[C.BLE_NPL_EINVAL]
	}
	if inISR() {
		return nplevent.ErrISR
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return controlErrors[C.BLE_NPL_ENOENT]
	}
	if err := b.event.Release(); err != nil {
		return err
	}
	if err := control(C.c6_public_unbind(&b.ref)); err != nil {
		return err
	}
	b.released = true
	return nil
}

func ABI() ([8]uint32, error) {
	var out [8]uint32
	if inISR() {
		return out, nplevent.ErrISR
	}
	var words [8]C.uint32_t
	C.c6_public_abi_read(&words[0])
	for i, word := range words {
		out[i] = uint32(word)
	}
	return out, nil
}
