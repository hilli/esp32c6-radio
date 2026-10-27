//go:build tinygo && esp32c6

package phy

/*
#cgo CFLAGS: -Wall -Wextra -Werror
#cgo LDFLAGS: -Lnative -lphy -lbtbb
#include "bridge.h"
*/
import "C"

import "machine"

// Calibrate performs a full RF calibration. The caller must power and clock the
// modem first. A result of 1 means calibration data was replaced, not failure.
func Calibrate() int { return int(C.esp32c6_phy_calibrate()) }

func InitBaseband() { C.esp32c6_baseband_init() }

func Close() { C.esp32c6_phy_close() }

//export esp32c6_phy_putchar
func phyPutchar(value C.uint8_t) C.int {
	if err := machine.Serial.WriteByte(byte(value)); err != nil {
		return -1
	}
	return 0
}
