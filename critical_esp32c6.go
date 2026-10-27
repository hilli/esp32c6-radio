//go:build tinygo && esp32c6 && esp32c6_radio

package esp32c6radio

import "device/riscv"

func lock() uintptr      { return riscv.DisableInterrupts() }
func unlock(key uintptr) { riscv.EnableInterrupts(key) }
