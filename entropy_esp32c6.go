//go:build tinygo && esp32c6 && esp32c6_radio

package esp32c6radio

import (
	"device/esp"
	"device/riscv"

	"github.com/hilli/gothread/platform"
	"github.com/hilli/gothread/radio"
)

var _ platform.Entropy = (*Radio)(nil)

func entropyCycles() uint32 { return uint32(riscv.CYCLE.Get()) }
func entropyRead() uint32   { return esp.LP_PERI.RNG_DATA.Get() }
func entropyTimer() uint32 {
	esp.LP_TIMER.SetUPDATE_MAIN_TIMER_UPDATE(1)
	return esp.LP_TIMER.MAIN_BUF0_LOW.Get()
}

// Fill reads the hardware RNG using the pinned vendor sampling procedure.
// Enable the radio BEFORE constructing an Instance that uses it as Entropy.
// MAC Sleep retains the PHY; Disable/Close invalidate this source. Physical
// entropy-source operation still requires board validation. Never call in ISR.
func (d *Radio) Fill(out []byte) error {
	d.entropyMu.Lock()
	defer d.entropyMu.Unlock()
	for len(out) > 0 {
		key := lock()
		if d.state == radio.StateDisabled || d.transition || d.closeRequested {
			unlock(key)
			return ErrEntropyUnavailable
		}
		epoch := d.rfEpoch
		unlock(key)
		value, err := sampleEntropy(&d.entropyLastCycle, entropyCycles, entropyRead, entropyTimer)
		if err != nil {
			return err
		}
		key = lock()
		valid := d.rfEpoch == epoch && d.state != radio.StateDisabled && !d.transition
		unlock(key)
		if !valid {
			return ErrEntropyUnavailable
		}
		count := min(4, len(out))
		for i := 0; i < count; i++ {
			out[i] = byte(value >> (8 * i))
		}
		out = out[count:]
	}
	return nil
}
