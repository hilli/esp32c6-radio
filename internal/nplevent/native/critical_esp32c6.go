//go:build tinygo && esp32c6

package native

import "runtime/interrupt"

func lock() uintptr      { return uintptr(interrupt.Disable()) }
func unlock(key uintptr) { interrupt.Restore(interrupt.State(key)) }
func inISR() bool        { return interrupt.In() }
