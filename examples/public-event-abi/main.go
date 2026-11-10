//go:build tinygo && esp32c6

// Compile/link witness only: no target execution, BIN or programming recipe.
package main

/*
#include "example.h"
*/
import "C"

import (
	"github.com/hilli/esp32c6-radio/internal/nplevent"
	"github.com/hilli/esp32c6-radio/internal/nplevent/publicabi"
)

func main() {
	abi, err := publicabi.ABI()
	if err != nil {
		C.c6p_example_record(0, 1)
		return
	}
	for i, word := range abi {
		C.c6p_example_record(C.uint32_t(i+1), C.uint32_t(word))
	}
	binding, err := publicabi.Bind(C.c6p_example_init())
	if err != nil {
		C.c6p_example_record(0, 2)
		return
	}
	q, err := nplevent.New(1)
	if err != nil {
		C.c6p_example_record(0, 3)
		return
	}
	if err := binding.Put(q); err != nil {
		C.c6p_example_record(0, 4)
		return
	}
	if ran, err := q.Dispatch(0); !ran || err != nil {
		C.c6p_example_record(0, 5)
		return
	}
	if err := binding.Put(q); err != (publicabi.Error{Code: 10}) {
		C.c6p_example_record(0, 6)
		return
	}
	if err := binding.Release(); err != nil {
		C.c6p_example_record(0, 7)
		return
	}
	C.c6p_example_raw_cycle()
	if C.c6p_example_check() != 0 {
		C.c6p_example_record(0, 9)
		return
	}
	if err := q.Close(); err != nil {
		C.c6p_example_record(0, 8)
		return
	}
	<-q.Done()
	C.c6p_example_record(0, 0)
}
