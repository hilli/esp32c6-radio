//go:build tinygo && esp32c6

// Compile/link witness only. This example is not authorized for execution,
// flashing or controller startup, and supplies no programming recipe.
package main

/*
#include "example.h"
*/
import "C"

import (
	"encoding/binary"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
	"github.com/hilli/esp32c6-radio/internal/nplevent/native"
)

func main() {
	result := uint32(1)
	abi, err := native.ABI()
	if err != nil {
		C.c6_example_record(0, C.uint32_t(result))
		return
	}
	for i, value := range abi {
		C.c6_example_record(C.uint32_t(i+1), C.uint32_t(value))
	}
	q, err := nplevent.New(1)
	if err != nil {
		C.c6_example_record(0, 2)
		return
	}
	var argument [8]byte
	binary.LittleEndian.PutUint32(argument[:], 0x6436c0de)
	binary.LittleEndian.PutUint32(argument[4:], uint32(0xffffffef))
	ev, err := native.New(native.Callback(C.c6_example_callback_address()), argument[:])
	if err != nil {
		C.c6_example_record(0, 3)
		return
	}
	if err := ev.Put(q); err != nil {
		C.c6_example_record(0, 4)
		return
	}
	ran, err := q.Dispatch(0)
	if !ran || err != (native.CallbackError{Status: -17}) {
		C.c6_example_record(0, 5)
		return
	}
	if err := ev.Put(q); err != nil {
		C.c6_example_record(0, 6)
		return
	}
	if ran, err = q.Dispatch(0); !ran || err != nil {
		C.c6_example_record(0, 7)
		return
	}
	status, err := ev.FirstFailure()
	C.c6_example_record(9, C.uint32_t(status))
	if err != nil || status != -17 {
		C.c6_example_record(0, 8)
		return
	}
	if err := q.Close(); err != nil {
		C.c6_example_record(0, 9)
		return
	}
	<-q.Done()
	if err := ev.Release(); err != nil {
		C.c6_example_record(0, 10)
		return
	}
	if _, err := ev.FirstFailure(); err != native.ErrStale {
		C.c6_example_record(0, 11)
		return
	}
	result = 0
	C.c6_example_record(0, C.uint32_t(result))
}
