//go:build tinygo && esp32c6 && !c6_owned_entry

package nplevent

import (
	"runtime/interrupt"
	"unsafe"
)

const pointerBytes = int(unsafe.Sizeof(uintptr(0)))

func lock() uintptr      { return uintptr(interrupt.Disable()) }
func unlock(key uintptr) { interrupt.Restore(interrupt.State(key)) }
func inISR() bool        { return interrupt.In() }
