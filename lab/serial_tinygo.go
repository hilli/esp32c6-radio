//go:build tinygo && esp32c6 && esp32c6_radio

package lab

import (
	"machine"
	"time"
)

// SerialReader adapts the explicitly selected firmware USB serial peripheral;
// host tools never discover or open arbitrary attached devices.
type SerialReader struct{}

func (SerialReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for machine.Serial.Buffered() == 0 {
		time.Sleep(time.Millisecond)
	}
	n := min(len(p), machine.Serial.Buffered())
	for i := 0; i < n; i++ {
		b, err := machine.Serial.ReadByte()
		if err != nil {
			return i, err
		}
		p[i] = b
	}
	return n, nil
}
