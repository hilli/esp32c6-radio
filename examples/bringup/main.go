//go:build tinygo && esp32c6

package main

import (
	"time"

	esp32c6radio "github.com/hilli/esp32c6-radio"
)

func main() {
	println("esp32c6-radio: native calibration/MAC IRQ probe, no packet RX/TX")
	calibration, err := esp32c6radio.Probe()
	if err != nil {
		println(err.Error(), "calibration result:", calibration)
		select {}
	}
	println("PHY/baseband initialization returned; calibration:", calibration)
	println("MAC timer interrupt observed")
	start := time.Now()
	for {
		time.Sleep(time.Second)
		println("uptime ms:", time.Since(start).Milliseconds())
	}
}
