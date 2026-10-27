package esp32c6radio

import (
	"bytes"
	"testing"

	"github.com/hilli/gothread/radio"
)

func macFixture() (*Radio, *c6Device) {
	simulatedMAC = [128]uint32{}
	h := &c6Device{lastRSSI: radio.RSSIInvalid}
	d := newRadio(h)
	h.owner = d
	d.state, d.bound = radio.StateReceive, true
	d.channel = 15
	h.receive(15, &d.cfg)
	return d, h
}

func fillRX(h *c6Device, payload []byte, rssi int8, lqi uint8) {
	raw := dmaBytes(&h.rxWords)
	raw[0] = byte(len(payload) + 2)
	copy(raw[1:], payload)
	raw[len(payload)+1] = byte(rssi)
	raw[len(payload)+2] = lqi
}

func TestMACACKTimeoutAndDuplicateEvents(t *testing.T) {
	d, h := macFixture()
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	if macReg(cmd).Get() != 0x41 {
		t.Fatal("wrong native TX opcode")
	}
	h.irq(txSFD | txDone)
	if !h.awaitAck || d.txEnded || macReg(cmd).Get() != 0x4c || macReg(timer0Threshold).Get() != 16000 {
		t.Fatal("ACK wait was not armed")
	}
	h.irq(timer0)
	if !d.txEnded || d.count != 2 || d.events[1].err != ErrNoAck {
		t.Fatal("ACK timeout not reported once")
	}
	h.irq(timer0 | txDone | txAbort | ackRXDone)
	if d.count != 2 {
		t.Fatal("duplicate completion")
	}
}

func TestMACAckWinsCoalescedEvents(t *testing.T) {
	d, h := macFixture()
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	h.irq(txDone)
	fillRX(h, []byte{0x12, 0, 7}, -82, 219)
	h.irq(ackRXDone | timer0 | txDone | rxDone)
	e := &d.events[1]
	if d.count != 2 || e.kind != eventDone || e.err != nil || !e.hasAck {
		t.Fatal("ACK completion lost or delivered as a data receive")
	}
	if !bytes.Equal(e.ack.PSDU, []byte{0x12, 0, 7}) || e.ackInfo.RSSI != -82 || e.ackInfo.LQI != 219 || !e.ackInfo.AckedWithFramePending {
		t.Fatal("ACK metadata normalization", e.ackInfo)
	}
}

func TestMACAbortMapping(t *testing.T) {
	for _, tc := range []struct {
		reason uint32
		want   error
	}{
		{0, ErrAbort}, {1, ErrAbort}, {16, ErrNoAck}, {17, ErrAbort}, {18, ErrAbort},
		{19, ErrUnsupported}, {24, ErrChannelAccessFailure}, {25, ErrChannelAccessFailure}, {31, ErrAbort},
	} {
		d, h := macFixture()
		if err := d.Transmit(txFrame()); err != nil {
			t.Fatal(err)
		}
		macReg(txStatus).Set(tc.reason << 4)
		h.irq(txAbort | txDone)
		if d.count != 2 || d.events[1].err != tc.want {
			t.Fatalf("reason %d: %v", tc.reason, d.events[1].err)
		}
	}
}

func TestMACReceiveHardwareACKAndFCSNormalization(t *testing.T) {
	d, h := macFixture()
	psdu := []byte{0x63, 0x98, 4, 0xcd, 0xab, 0x34, 0x12, 0x78, 0x56, 4}
	fillRX(h, psdu, -91, 237)
	h.rxTimestamp = 42
	h.irq(rxDone)
	if !h.sendingAck || d.count != 0 || macReg(pendingConfig).Get()&1 == 0 {
		t.Fatal("did not leave ACK turnaround to hardware")
	}
	h.irq(ackTXDone)
	if d.count != 1 || h.sendingAck || macReg(cmd).Get() != 0x42 {
		t.Fatal("did not rearm RX after hardware ACK")
	}
	e := &d.events[0]
	if !bytes.Equal(e.frame.PSDU, psdu) || e.rxInfo.RSSI != -91 || e.rxInfo.LQI != 237 || e.rxInfo.Timestamp != 42 {
		t.Fatal("RX/FCS normalization")
	}
	fillRX(h, []byte{1, 0x10, 99}, -20, 1)
	if !bytes.Equal(e.frame.PSDU, psdu) {
		t.Fatal("DMA reuse overwrote queued receive")
	}
}

func TestMACLengthBoundariesAndCCA(t *testing.T) {
	d, h := macFixture()
	for _, size := range []int{3, 125} {
		payload := make([]byte, size)
		payload[0], payload[1] = 1, 0x10
		fillRX(h, payload, -73, 255)
		frame, info, ok := h.received()
		if !ok || len(frame) != size || info.RSSI != -73 || info.LQI != 255 {
			t.Fatal(size)
		}
	}
	for _, size := range []byte{0, 1, 2, 3, 4, 128} {
		dmaBytes(&h.rxWords)[0] = size
		if _, _, ok := h.received(); ok {
			t.Fatal("accepted invalid PHY length", size)
		}
	}
	f := &radio.Frame{PSDU: make([]byte, 125), Channel: 26, TxInfo: &radio.TxInfo{CsmaCaEnabled: true, TxPower: radio.PowerInvalid}}
	f.PSDU[0], f.PSDU[1] = 1, 0x10
	if err := d.Transmit(f); err != nil {
		t.Fatal(err)
	}
	if dmaBytes(&d.txWords)[0] != 127 || macReg(cmd).Get() != 0x43 || macReg(0x48).Get() != 78 || macReg(0x50).Get() != 8 {
		t.Fatal("max PSDU/channel/CCA encoding")
	}
}

func TestMACFilteringPowerAndFactoryEUI(t *testing.T) {
	d, h := macFixture()
	d.cfg = configuration{pan: 0xabcd, short: 0x1234, extended: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, power: 20}
	macReg(conf).Set(0x5a << 16)
	macReg(pendingConfig).Set(0x00800000)
	macReg(0x5c).Set(0x0360)
	h.receive(11, &d.cfg)
	if macReg(0x08).Get() != 0x1234 || macReg(0x0c).Get() != 0xabcd || macReg(0x10).Get() != 0x05060708 || macReg(0x14).Get() != 0x01020304 || macReg(0x4c).Get() != 15 {
		t.Fatal("filter/power byte order")
	}
	if macReg(conf).Get()&(1<<12) == 0 || macReg(conf).Get()&0xff0000 != 0x5a0000 || macReg(pendingConfig).Get() != 0x00800000 {
		t.Fatal("Thread pending policy or hardware reset timing fields were lost")
	}
	if macReg(0x5c).Get() != 0x0360 {
		t.Fatal("unrelated hardware ACK timing was overwritten")
	}
	d.cfg.promiscuous = true
	h.receive(11, &d.cfg)
	if macReg(conf).Get()&1 != 0 || macReg(conf).Get()&(1<<7) == 0 || macReg(0x128).Get() != 0 {
		t.Fatal("promiscuous ACK or security setup")
	}
	if eui := factoryEUI(0x33445566, 0xfffe1122); eui != [8]byte{0x11, 0x22, 0x33, 0xff, 0xfe, 0x44, 0x55, 0x66} {
		t.Fatal(eui)
	}
}

func TestMACDefersConfigurationUntilHardwareACKCompletes(t *testing.T) {
	d, h := macFixture()
	h.receive(15, &d.cfg)
	fillRX(h, []byte{0x21, 0x10, 3}, -70, 100)
	h.irq(rxDone)
	if !h.sendingAck {
		t.Fatal("hardware ACK was not in progress")
	}
	d.SetPromiscuous(true)
	d.SetPanID(0xbeef)
	if d.Sleep() != ErrBusy || d.EnergyScan(15, 100) != ErrBusy || d.Receive(16) != ErrBusy {
		t.Fatal("state transition interrupted hardware ACK")
	}
	if err := d.Receive(15); err != nil {
		t.Fatal("same-channel receive must leave an in-progress ACK alone", err)
	}
	if h.promiscuous || macReg(0x0c).Get() == 0xbeef {
		t.Fatal("configuration interrupted hardware ACK")
	}
	h.irq(ackTXDone)
	if !h.promiscuous || macReg(0x0c).Get() != 0xbeef || d.count != 1 {
		t.Fatal("deferred configuration not applied after ACK")
	}
}

func TestMACExpectedStopsAndScanAbort(t *testing.T) {
	d, h := macFixture()
	if err := d.EnergyScan(15, 100); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []uint32{1, 17, 25} {
		macReg(rxStatus).Set(reason << 4)
		h.irq(rxAbort)
		if macReg(cmd).Get() != 0x44 || !d.scanning || d.count != 0 || d.stats.ReceiveInvalid != 0 {
			t.Fatal("expected STOP outcome interrupted a new operation", reason)
		}
	}
	macReg(rxStatus).Set(24 << 4)
	h.irq(rxAbort)
	if d.scanning || d.count != 1 || d.events[0].rssi != radio.RSSIInvalid {
		t.Fatal("ED abort did not report an explicit invalid measurement")
	}
}

func TestMACDoesNotDeliverStaleRXInOtherOperations(t *testing.T) {
	d, h := macFixture()
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	fillRX(h, []byte{1, 0x10, 3}, -70, 100)
	h.irq(rxDone)
	if d.count != 1 || d.txEnded {
		t.Fatal("stale RX aborted an accepted transmit")
	}
	d, h = macFixture()
	if err := d.EnergyScan(15, 100); err != nil {
		t.Fatal(err)
	}
	fillRX(h, []byte{1, 0x10, 3}, -70, 100)
	h.irq(rxDone)
	if d.count != 0 || !d.scanning {
		t.Fatal("stale RX escaped an energy scan")
	}
}
func TestMACScanAndISRAllocations(t *testing.T) {
	d, h := macFixture()
	d.scanning, d.reserved = true, 1
	macReg(0x54).Set(uint32(166) << 16)
	h.irq(edDone)
	if d.count != 1 || d.events[0].rssi != -90 || d.scanning {
		t.Fatal("energy scan normalization")
	}
	fillRX(h, []byte{0x21, 0x10, 3}, -70, 100)
	allocations := testing.AllocsPerRun(1000, func() {
		d.head, d.tail, d.count, d.reserved = 0, 0, 0, 0
		d.txPending, d.txEnded = false, false
		h.irq(rxDone | ackTXDone)
	})
	if allocations != 0 {
		t.Fatalf("ISR allocated %g objects", allocations)
	}
}
