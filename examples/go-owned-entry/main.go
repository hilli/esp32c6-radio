//go:build tinygo && esp32c6 && c6_owned_entry

// Compile/link probe only; not authorized for execution, BIN or flashing.
package main

/*
#include "example.h"
*/
import "C"

import (
	"errors"
	"runtime"

	"github.com/hilli/esp32c6-radio/internal/nplevent"
	"github.com/hilli/esp32c6-radio/internal/nplevent/owned"
	"github.com/hilli/esp32c6-radio/internal/nplevent/publicabi"
)

func main() {
	if _, err := owned.Call(1); err != owned.ErrUnowned {
		C.c6o_example_record(0, 1)
		return
	}
	entered, permit := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	first := errors.New("late owned yield probe")
	var aIdentity uintptr
	go func() {
		done <- owned.With(func() error {
			b, err := publicabi.Bind(C.c6o_example_init())
			if err != nil {
				return err
			}
			q, err := nplevent.New(1)
			if err != nil {
				return err
			}
			if err := b.Put(q); err != nil {
				return err
			}
			if ran, err := q.Dispatch(0); !ran || err != nil {
				return errors.New("owned probe dispatch")
			}
			if err := b.Release(); err != nil {
				return err
			}
			return q.Close()
		}, func(uint32) (int32, error) {
			aIdentity = owned.Identity()
			C.c6o_example_record(1, C.uint32_t(aIdentity))
			close(entered)
			runtime.Gosched()
			<-permit
			if owned.Identity() != aIdentity || C.c6o_example_retire() != 0 {
				return 0, errors.New("owned probe self after yield")
			}
			return -2147483648, first
		})
	}()
	select {
	case <-entered:
	case <-done:
		C.c6o_example_record(0, 3)
		return
	}
	err := owned.With(func() error {
		identity := owned.Identity()
		C.c6o_example_record(2, C.uint32_t(identity))
		if identity == 0 || identity == aIdentity || C.c6o_example_foreign_retire() != 11 {
			return errors.New("owned probe foreign")
		}
		status, err := owned.Call(2)
		if err != nil || status != 42 {
			return errors.New("owned probe second return")
		}
		return nil
	}, func(uint32) (int32, error) { runtime.Gosched(); return 42, nil })
	close(permit)
	aErr := <-done
	if err != nil || aErr != first || C.c6o_example_check() != 0 {
		C.c6o_example_record(0, 2)
		return
	}
	C.c6o_example_record(0, 0)
}
