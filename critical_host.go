//go:build !tinygo || !esp32c6 || !esp32c6_radio

package esp32c6radio

import "sync"

var critical sync.Mutex

func lock() uintptr  { critical.Lock(); return 0 }
func unlock(uintptr) { critical.Unlock() }

func newDevice() (device, error) { return nil, ErrUnsupported }

// Unsupported-target register storage exercises the C6 IRQ state machine on host.
// Public construction still fails; this is never an operational host radio.
var simulatedMAC [128]uint32

func readMAC(offset uintptr) uint32 { return simulatedMAC[offset/4] }
func writeMAC(offset uintptr, value uint32) {
	if offset == eventStatus {
		simulatedMAC[offset/4] &^= value
	} else {
		simulatedMAC[offset/4] = value
	}
}
func dmaFence()                         {}
func readFactoryMAC() (uint32, uint32)  { return 0, 0 }
func (h *c6Device) enable(*Radio) error { return ErrUnsupported }
func (h *c6Device) disable() error      { return ErrUnsupported }
func (d *Radio) Fill([]byte) error      { return ErrUnsupported }
