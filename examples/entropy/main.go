//go:build tinygo && esp32c6 && esp32c6_radio

package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/esp32c6-radio/lab"
)

var firmwareID = "UNSET"

func main() {
	driver, err := esp32c6radio.New()
	if err != nil {
		println(err.Error())
		select {}
	}
	eui := driver.GetIEEEEui64()
	identity := hex.EncodeToString(eui[:])
	input := bufio.NewScanner(lab.SerialReader{})
	input.Buffer(make([]byte, 1024), 1024)
	session := lab.Session{FirmwareID: firmwareID, DeviceEUI: identity}
	for input.Scan() {
		c, err := lab.ParseCommand(input.Bytes())
		if err != nil || session.Arm(c) != nil {
			println("entropy identity/scope not armed")
			continue
		}
		e := lab.Event{Probe: "entropy", Stage: "hello", Status: lab.Pass}
		if c.Operation != "hello" {
			e.Stage = "health"
			if c.Operation != "run" || c.Confirmation != identity+":phy-entropy:"+c.RunID {
				e.Status = lab.Fail
				e.Reason = "explicit calibration/entropy scope required"
			} else {
				err := driver.Enable()
				if err != nil {
					e.Status = lab.Fail
					e.Reason = err.Error()
				} else {
					source := driver.EntropyState()
					buffer := make([]byte, 8192)
					err = driver.Fill(buffer)
					if err != nil {
						e.Status = lab.Fail
						e.Reason = err.Error()
					} else {
						h, status := lab.SmokeHealth(buffer)
						e.Status = status
						e.Metrics = map[string]uint64{"samples": h.Samples, "ones": h.Ones, "longest_byte_run": h.LongestByteRun, "repeated_words": h.RepeatedWords}
						// Only a digest is emitted, never raw entropy or credentials.
						e.Digest = h.Digest
						e.Reason = "smoke only; source qualification NOT_RUN"
						if source.PHYActive {
							e.Metrics["phy_active"] = 1
						}
						if source.FrontEndClocks {
							e.Metrics["front_end_clocks"] = 1
						}
						if source.RNGClock {
							e.Metrics["rng_clock"] = 1
						}
						e.Metrics["cpu_mhz"] = uint64(source.CPUMHz)
						e.Metrics["apb_mhz"] = uint64(source.APBMHz)
						if !source.PHYActive || !source.FrontEndClocks || !source.RNGClock {
							e.Status = lab.Fail
							e.Reason = "digital source prerequisites missing"
						}
					}
					clear(buffer)
				}
			}
		}
		p, err := session.Record(e)
		if err != nil {
			println("entropy evidence encoding failed")
			continue
		}
		fmt.Fprint(os.Stdout, string(p))
	}
	if err := input.Err(); err != nil {
		println(err.Error())
	}
	select {}
}
