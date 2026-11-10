//go:build tinygo && esp32c6 && !c6_owned_entry

package publicabi

import "runtime/interrupt"

func inISR() bool { return interrupt.In() }
