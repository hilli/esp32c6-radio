//go:build tinygo && esp32c6 && esp32c6_radio

package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/esp32c6-radio/lab"
	"github.com/hilli/gothread/mac/types"
	"github.com/hilli/gothread/radio"
)

var firmwareID = "UNSET"

type receiver struct {
	done                                                                          chan struct{}
	receivedAll                                                                   chan struct{}
	seen                                                                          [10000]bool
	peer                                                                          uint16
	target                                                                        uint32
	run                                                                           [8]byte
	accepted, started, terminal, success, noack, cca, aborted, unique, duplicates uint64
	rssiMin, rssiMax                                                              int8
	lqiMin, lqiMax                                                                uint8
}

func (r *receiver) HandleReceiveDone(f *radio.Frame, err error) {
	if err != nil {
		return
	}
	p := f.PSDU
	if len(p) != 21 || p[9] != 'C' || p[10] != '6' || uint16(p[7])|uint16(p[8])<<8 != r.peer {
		return
	}
	for i := range r.run {
		if p[13+i] != r.run[i] {
			return
		}
	}
	n := uint32(p[11]) | uint32(p[12])<<8
	if n >= r.target {
		return
	}
	if r.seen[n] {
		r.duplicates++
		return
	}
	r.seen[n] = true
	r.unique++
	if r.unique == 1 {
		r.rssiMin, r.rssiMax = f.RxInfo.RSSI, f.RxInfo.RSSI
		r.lqiMin, r.lqiMax = f.RxInfo.LQI, f.RxInfo.LQI
	}
	r.rssiMin = min(r.rssiMin, f.RxInfo.RSSI)
	r.rssiMax = max(r.rssiMax, f.RxInfo.RSSI)
	r.lqiMin = min(r.lqiMin, f.RxInfo.LQI)
	r.lqiMax = max(r.lqiMax, f.RxInfo.LQI)
	if r.unique == uint64(r.target) {
		close(r.receivedAll)
	}
}
func (r *receiver) HandleTransmitStarted(*radio.Frame) { r.started++ }
func (r *receiver) HandleTransmitDone(_ *radio.Frame, _ *radio.Frame, errorValue error) {
	r.terminal++
	switch errorValue {
	case nil:
		r.success++
	case esp32c6radio.ErrNoAck:
		r.noack++
	case esp32c6radio.ErrChannelAccessFailure:
		r.cca++
	default:
		r.aborted++
	}
	r.done <- struct{}{}
}
func (*receiver) HandleEnergyScanDone(int8) {}

func run(d *esp32c6radio.Radio, c lab.Command) (map[string]uint64, error) {
	if c.Channel < 11 || c.Channel > 26 || c.PanID == 0 || c.PanID == 0xffff || c.LocalAddress == c.PeerAddress || c.LocalAddress == 0xffff || c.PeerAddress == 0xffff || c.Count < 1000 || c.Count > 10000 {
		return nil, esp32c6radio.ErrInvalidArgument
	}
	r := &receiver{done: make(chan struct{}, 1), receivedAll: make(chan struct{}), peer: c.PeerAddress, target: c.Count}
	runID, err := hex.DecodeString(c.RunID)
	if err != nil || len(runID) < 8 {
		return nil, esp32c6radio.ErrInvalidArgument
	}
	copy(r.run[:], runID[:8])
	if err := d.SetCallbacks(r, nil); err != nil {
		return nil, err
	}
	if err := d.Enable(); err != nil {
		return nil, err
	}
	d.SetPanID(c.PanID)
	d.SetShortAddress(types.ShortAddress(c.LocalAddress))
	if err := d.SetTransmitPower(0); err != nil {
		return nil, err
	}
	if err := d.Receive(c.Channel); err != nil {
		return nil, err
	}
	f := d.GetTransmitBuffer()
	f.Channel = c.Channel
	f.PSDU = append(f.PSDU[:0], 0x61, 0x98, 0, byte(c.PanID), byte(c.PanID>>8), byte(c.PeerAddress), byte(c.PeerAddress>>8), byte(c.LocalAddress), byte(c.LocalAddress>>8), 'C', '6', 0, 0)
	f.PSDU = append(f.PSDU, r.run[:]...)
	f.TxInfo.CsmaCaEnabled = true
	for i := uint32(0); i < c.Count; i++ {
		f.PSDU[2], f.PSDU[11], f.PSDU[12] = byte(i), byte(i), byte(i>>8)
		deadline := time.Now().Add(time.Second)
		for {
			err := d.Transmit(f)
			if err != esp32c6radio.ErrBusy || time.Now().After(deadline) {
				if err != nil {
					return nil, err
				}
				break
			}
			time.Sleep(time.Millisecond)
		}
		r.accepted++
		select {
		case <-r.done:
		case <-time.After(time.Second):
			return nil, fmt.Errorf("missing terminal callback")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-r.receivedAll:
	case <-time.After(30 * time.Second):
	}
	if err := d.Close(); err != nil {
		return nil, err
	}
	<-d.Done()
	stats := d.Statistics()
	return map[string]uint64{
		"accepted": r.accepted, "started": r.started, "terminal": r.terminal, "success": r.success, "noack": r.noack, "cca": r.cca, "abort": r.aborted,
		"rx_unique": r.unique, "rx_duplicates": r.duplicates, "rx_overflow": stats.ReceiveDropped, "rx_invalid": stats.ReceiveInvalid,
		"rx_unsupported": stats.ReceiveUnsupported, "channel": uint64(c.Channel), "rssi_min_plus128": uint64(int16(r.rssiMin) + 128),
		"rssi_max_plus128": uint64(int16(r.rssiMax) + 128), "lqi_min": uint64(r.lqiMin), "lqi_max": uint64(r.lqiMax),
		"sniffer_ack_timing_artifact": 0, // never invent external RF/FCS/timing evidence
	}, nil
}
func main() {
	d, err := esp32c6radio.New()
	if err != nil {
		println(err.Error())
		select {}
	}
	eui := d.GetIEEEEui64()
	session := lab.Session{FirmwareID: firmwareID, DeviceEUI: hex.EncodeToString(eui[:])}
	s := bufio.NewScanner(lab.SerialReader{})
	s.Buffer(make([]byte, 2048), 2048)
	for s.Scan() {
		c, err := lab.ParseCommand(s.Bytes())
		if err != nil {
			println("invalid RF command")
			continue
		}
		if session.Arm(c) != nil {
			println("RF identity not armed")
			continue
		}
		e := lab.Event{Probe: "rf", Stage: "hello", Status: lab.Pass}
		if c.Operation != "hello" {
			e.Stage = "summary"
			if c.Operation != "run" || c.Confirmation != session.DeviceEUI+":rf:"+session.RunID {
				e.Status = lab.Fail
				e.Reason = "explicit RF device/channel scope required"
			} else {
				metrics, err := run(d, c)
				e.Metrics = metrics
				if err != nil {
					e.Status = lab.Fail
					e.Reason = err.Error()
				} else {
					e.Status = lab.Pass
					if metrics["rx_unique"] < uint64(c.Count) || metrics["started"] != metrics["accepted"] || metrics["terminal"] != metrics["accepted"] {
						e.Status = lab.Fail
					}
					e.Reason = "software counters only; external ACK/FCS/sniffer timing evidence required"
				}
			}
		}
		p, err := session.Record(e)
		if err != nil {
			println("RF evidence encoding failed")
			continue
		}
		fmt.Fprint(os.Stdout, string(p))
	}
	if err := s.Err(); err != nil {
		println(err.Error())
	}
	select {}
}
