//go:build tinygo && esp32c6 && esp32c6_radio

// This laboratory example sends unsecured unicast test frames. It does not
// provision Thread credentials or touch flash.
package main

import (
	"strconv"
	"time"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/gothread/mac/types"
	"github.com/hilli/gothread/radio"
)

var localAddress = "0x1234"
var peerAddress = "0x5678"

type receiver struct {
	done                                                                     chan struct{}
	allReceived                                                              chan struct{}
	seen                                                                     [1000]bool
	received, duplicates, started, completed, succeeded, noACK, cca, aborted uint32
	peer                                                                     uint16
}

func (r *receiver) HandleReceiveDone(frame *radio.Frame, err error) {
	if err != nil {
		println("RX error:", err.Error())
		return
	}
	p := frame.PSDU
	if len(p) != 13 || p[9] != 'C' || p[10] != '6' || uint16(p[7])|uint16(p[8])<<8 != r.peer {
		return
	}
	sequence := uint16(p[11]) | uint16(p[12])<<8
	if sequence >= 1000 {
		return
	}
	if r.seen[sequence] {
		r.duplicates++
		return
	}
	r.seen[sequence] = true
	r.received++
	if r.received == 1000 {
		close(r.allReceived)
	}
}
func (r *receiver) HandleTransmitStarted(*radio.Frame) { r.started++ }
func (r *receiver) HandleTransmitDone(_ *radio.Frame, _ *radio.Frame, err error) {
	r.completed++
	switch err {
	case nil:
		r.succeeded++
	case esp32c6radio.ErrNoAck:
		r.noACK++
	case esp32c6radio.ErrChannelAccessFailure:
		r.cca++
	default:
		r.aborted++
		println("TX error:", err.Error())
	}
	r.done <- struct{}{}
}
func (*receiver) HandleEnergyScanDone(rssi int8) { println("ED maximum:", rssi) }

func check(err error) {
	if err != nil {
		println(err.Error())
		select {}
	}
}

func main() {
	local, err := strconv.ParseUint(localAddress, 0, 16)
	check(err)
	peer, err := strconv.ParseUint(peerAddress, 0, 16)
	check(err)
	if local == peer || local == 0xffff || peer == 0xffff {
		println("choose distinct unicast addresses")
		select {}
	}
	driver, err := esp32c6radio.New()
	check(err)
	events := &receiver{done: make(chan struct{}, 1), allReceived: make(chan struct{}), peer: uint16(peer)}
	check(driver.SetCallbacks(events, nil))
	check(driver.Enable()) // full native calibration plus a real MAC IRQ probe
	driver.SetPanID(0xcafe)
	driver.SetShortAddress(types.ShortAddress(local))
	driver.SetExtendedAddress(driver.GetIEEEEui64())
	check(driver.SetTransmitPower(0))
	check(driver.Receive(15))
	println("experimental raw radio; RF behavior has not been validated")
	frame := driver.GetTransmitBuffer()
	frame.Channel = 15
	// Legacy data + ACK request, compressed PAN, short destination/source.
	frame.PSDU = append(frame.PSDU[:0], 0x61, 0x98, 0, 0xfe, 0xca,
		byte(peer), byte(peer>>8), byte(local), byte(local>>8), 'C', '6', 0, 0)
	frame.TxInfo.CsmaCaEnabled = true
	for sequence := 0; sequence < 1000; sequence++ {
		frame.PSDU[2] = byte(sequence)
		frame.PSDU[11], frame.PSDU[12] = byte(sequence), byte(sequence>>8)
		deadline := time.Now().Add(time.Second)
		for {
			err := driver.Transmit(frame)
			if err != esp32c6radio.ErrBusy || !time.Now().Before(deadline) {
				check(err)
				break
			}
			time.Sleep(time.Millisecond)
		}
		<-events.done
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case <-events.allReceived:
	case <-time.After(30 * time.Second):
		println("peer RX drain deadline reached")
	}
	check(driver.Close())
	<-driver.Done()
	stats := driver.Statistics()
	println("TX accepted: 1000 started:", events.started, "terminal:", events.completed, "success:", events.succeeded)
	println("TX noACK:", events.noACK, "CCA:", events.cca, "abort/other:", events.aborted)
	println("RX unique:", events.received, "duplicates:", events.duplicates, "drops:", stats.ReceiveDropped, "invalid:", stats.ReceiveInvalid)
	println("radio quiesced; callback queue drained")
	select {}
}
