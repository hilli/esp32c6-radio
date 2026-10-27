//go:build tinygo && esp32c6 && esp32c6_radio

package esp32c6radio

import "github.com/hilli/esp32c6-radio/internal/chip"

// Probe initializes the native PHY and baseband, then checks a real MAC timer
// interrupt. It is an exclusive, one-shot diagnostic, not a radio driver.
// The calibration result is the unmodified vendor return value.
func Probe() (calibration int, err error) {
	calibration, err = chip.Init()
	if err != nil {
		return calibration, err
	}
	return calibration, chip.ProbeIRQ()
}
