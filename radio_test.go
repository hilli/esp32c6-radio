package esp32c6radio

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/hilli/gothread/mac/submac"
	"github.com/hilli/gothread/mac/types"
	"github.com/hilli/gothread/radio"
)

type fakeDevice struct {
	calls                    int
	blocked                  bool
	initError, shutdownError error
	words                    [33]uint32
	lastCCA                  bool
	lastChannel              uint8
	lastPower                int8
}

func (h *fakeDevice) enable(*Radio) error           { h.calls++; return h.initError }
func (h *fakeDevice) disable() error                { return h.shutdownError }
func (h *fakeDevice) stop()                         {}
func (h *fakeDevice) busy() bool                    { return h.blocked }
func (h *fakeDevice) idle() bool                    { return !h.blocked }
func (h *fakeDevice) receive(uint8, *configuration) { h.calls++ }
func (h *fakeDevice) transmit(words *[33]uint32, channel uint8, cfg *configuration, cca bool) {
	h.calls++
	h.words = *words
	h.lastCCA, h.lastChannel, h.lastPower = cca, channel, cfg.power
}
func (h *fakeDevice) scan(uint8, uint32, *configuration) { h.calls++ }
func (h *fakeDevice) factoryEUI() [8]byte                { return [8]byte{0x60, 2, 3, 0xff, 0xfe, 4, 5, 6} }
func (h *fakeDevice) rssi() int8                         { return -63 }

type notice struct {
	kind      string
	data, ack []byte
	err       error
	info      radio.RxInfo
}
type callbacks struct {
	got     chan notice
	receive func(*radio.Frame, error)
	done    func(*radio.Frame, *radio.Frame, error)
}

func (c *callbacks) HandleReceiveDone(frame *radio.Frame, err error) {
	if c.receive != nil {
		c.receive(frame, err)
	}
	n := notice{kind: "rx", err: err}
	if frame != nil {
		n.data = append([]byte(nil), frame.PSDU...)
		n.info = *frame.RxInfo
	}
	c.got <- n
}
func (c *callbacks) HandleTransmitStarted(frame *radio.Frame) {
	c.got <- notice{kind: "start", data: append([]byte(nil), frame.PSDU...)}
}
func (c *callbacks) HandleTransmitDone(frame, ack *radio.Frame, err error) {
	if c.done != nil {
		c.done(frame, ack, err)
	}
	n := notice{kind: "done", data: append([]byte(nil), frame.PSDU...), err: err}
	if ack != nil {
		n.ack = append([]byte(nil), ack.PSDU...)
	}
	c.got <- n
}
func (c *callbacks) HandleEnergyScanDone(rssi int8) {
	c.got <- notice{kind: "scan", info: radio.RxInfo{RSSI: rssi}}
}

func setup(t *testing.T, cb *callbacks, post func(func())) (*Radio, *fakeDevice) {
	t.Helper()
	h := &fakeDevice{}
	d := newRadio(h)
	if cb.got == nil {
		cb.got = make(chan notice, 64)
	}
	if err := d.SetCallbacks(cb, post); err != nil {
		t.Fatal(err)
	}
	if err := d.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := d.Receive(15); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-d.Done():
		case <-time.After(time.Second):
			t.Error("worker did not drain")
		}
	})
	return d, h
}

func next(t *testing.T, cb *callbacks, kind string) notice {
	t.Helper()
	select {
	case n := <-cb.got:
		if n.kind != kind {
			t.Fatalf("got %q, want %q", n.kind, kind)
		}
		return n
	case <-time.After(time.Second):
		t.Fatalf("no %s callback", kind)
		return notice{}
	}
}

func txFrame() *radio.Frame { return &radio.Frame{PSDU: []byte{0x21, 0x10, 7}, Channel: 15} }

func TestUnsupportedHostAndCapabilities(t *testing.T) {
	if d, err := New(); d != nil || err != ErrUnsupported {
		t.Fatal(d, err)
	}
	d := newRadio(&fakeDevice{})
	if d.GetCaps() != radio.CapAckTimeout|radio.CapEnergyScan {
		t.Fatal(d.GetCaps())
	}
	if d.GetCaps().Has(radio.CapTransmitSec) {
		t.Fatal("hardware encryption advertised")
	}
	if d.GetSupportedChannelMask() != 0x07fff800 {
		t.Fatal("wrong channels")
	}
	if d.GetRSSI() != radio.RSSIInvalid {
		t.Fatal("RSSI while disabled")
	}
	if d.cfg.extended != types.ExtAddress(d.GetIEEEEui64()) {
		t.Fatal("factory address not applied")
	}
}

func TestValidationBeforeHardware(t *testing.T) {
	cb := &callbacks{}
	d, h := setup(t, cb, nil)
	before := h.calls
	for _, channel := range []uint8{0, 10, 27, 255} {
		if d.Receive(channel) != ErrInvalidArgument {
			t.Fatal(channel)
		}
		if d.EnergyScan(channel, 1) != ErrInvalidArgument {
			t.Fatal(channel)
		}
	}
	for _, size := range []int{0, 1, 2, 126, 127, 128} {
		if d.Transmit(&radio.Frame{PSDU: make([]byte, size), Channel: 15}) != ErrInvalidArgument {
			t.Fatal(size)
		}
	}
	frame := txFrame()
	frame.PSDU[1] = 0x20
	if d.Transmit(frame) != ErrUnsupported {
		t.Fatal("accepted enhanced ACK frame")
	}
	frame = txFrame()
	frame.PSDU[0] |= 8
	if d.Transmit(frame) != ErrUnsupported {
		t.Fatal("accepted unencrypted secured frame")
	}
	for _, power := range []int8{-16, -14, 19, 21, 127} {
		if d.SetTransmitPower(power) != ErrInvalidArgument {
			t.Fatal(power)
		}
	}
	if d.EnergyScan(15, 0) != ErrInvalidArgument || d.EnergyScan(15, 16778) != ErrUnsupported {
		t.Fatal("scan bounds")
	}
	if h.calls != before {
		t.Fatal("invalid input reached hardware")
	}
}

func TestTransmitOwnershipAndTerminalIdentity(t *testing.T) {
	cb := &callbacks{}
	d, h := setup(t, cb, nil)
	frame := txFrame()
	want := append([]byte(nil), frame.PSDU...)
	if err := d.Transmit(frame); err != nil {
		t.Fatal(err)
	}
	raw := dmaBytes(&h.words)
	if raw[0] != 5 || !bytes.Equal(raw[1:4], want) || raw[4] != 0 || raw[5] != 0 {
		t.Fatal(raw[:6])
	}
	if uintptr(unsafe.Pointer(&d.txWords))%4 != 0 {
		t.Fatal("unaligned DMA")
	}
	frame.PSDU[2] = 99
	ack := []byte{2, 0x10, 7}
	key := lock()
	d.transmitDone(ack, radio.RxInfo{RSSI: -70}, ErrNoAck)
	d.transmitDone(nil, radio.RxInfo{}, ErrAbort)
	unlock(key)
	ack[2] = 42
	if n := next(t, cb, "start"); !bytes.Equal(n.data, want) {
		t.Fatal(n)
	}
	if n := next(t, cb, "done"); !bytes.Equal(n.data, want) || !bytes.Equal(n.ack, []byte{2, 0x10, 7}) || n.err != submac.ErrNoAck {
		t.Fatal(n)
	}
	key = lock()
	pending := d.txPending
	unlock(key)
	if pending {
		t.Fatal("completion did not release transmit ownership")
	}
}

func TestReceiveLifetimeAndMetadata(t *testing.T) {
	cb := &callbacks{}
	tasks := make(chan func(), 1)
	d, _ := setup(t, cb, func(fn func()) { tasks <- fn })
	payload := []byte{1, 0x10, 8, 42}
	info := radio.RxInfo{Timestamp: 1234, RSSI: -82, LQI: 231, AckedWithFramePending: true}
	key := lock()
	d.receiveDone(payload, 26, info)
	unlock(key)
	fn := <-tasks
	payload[3] = 99
	fn()
	n := next(t, cb, "rx")
	if !bytes.Equal(n.data, []byte{1, 0x10, 8, 42}) || n.info != info {
		t.Fatal(n)
	}
}

func TestPressureReservesTXAndSurfacesOverflow(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	var seen atomic.Int32
	cb := &callbacks{receive: func(*radio.Frame, error) {
		if seen.Add(1) == 1 {
			close(first)
			<-release
		}
	}}
	d, _ := setup(t, cb, nil)
	key := lock()
	d.receiveDone([]byte{1, 0x10, 1}, 15, radio.RxInfo{})
	unlock(key)
	<-first
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	key = lock()
	for i := 0; i < 100; i++ {
		d.receiveDone([]byte{1, 0x10, byte(i)}, 15, radio.RxInfo{})
	}
	d.transmitDone(nil, radio.RxInfo{}, ErrChannelAccessFailure)
	count, dropped := d.count, d.stats.ReceiveDropped
	unlock(key)
	close(release)
	if count != eventCount || dropped != 87 {
		t.Fatalf("ring count %d, dropped %d", count, dropped)
	}
	next(t, cb, "rx")
	next(t, cb, "start")
	done, overflow := false, false
	for !done || !overflow {
		select {
		case n := <-cb.got:
			if n.kind == "done" {
				if n.err != submac.ErrChannelAccessFailure {
					t.Fatal(n.err)
				}
				if done {
					t.Fatal("duplicate completion")
				}
				done = true
			}
			if n.err == ErrOverflow {
				overflow = true
			}
		case <-time.After(time.Second):
			t.Fatal("reserved completion or overflow notification was lost")
		}
	}
}

func TestCallbackReentrancyAndCloseDrain(t *testing.T) {
	var d *Radio
	cb := &callbacks{}
	cb.done = func(frame, _ *radio.Frame, _ error) {
		want := append([]byte(nil), frame.PSDU...)
		if err := d.Transmit(txFrame()); err != nil {
			t.Error(err)
		}
		if !bytes.Equal(frame.PSDU, want) {
			t.Error("reentrant TX overwrote borrowed completion")
		}
		if err := d.Close(); err != nil {
			t.Error(err)
		}
		cb.done = nil
	}
	d, _ = setup(t, cb, nil)
	if err := d.Transmit(txFrame()); err != nil {
		t.Fatal(err)
	}
	key := lock()
	d.transmitDone(nil, radio.RxInfo{}, nil)
	unlock(key)
	next(t, cb, "start")
	next(t, cb, "done")
	next(t, cb, "start")
	if n := next(t, cb, "done"); n.err != ErrAbort {
		t.Fatal(n)
	}
	select {
	case <-d.Done():
	case <-time.After(time.Second):
		t.Fatal("close from callback deadlocked")
	}
	if err := d.Enable(); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestDisableEnableAndScan(t *testing.T) {
	cb := &callbacks{}
	d, _ := setup(t, cb, nil)
	if err := d.Disable(); err != nil {
		t.Fatal(err)
	}
	if d.IsEnabled() {
		t.Fatal("not disabled")
	}
	if err := d.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := d.Receive(26); err != nil {
		t.Fatal(err)
	}
	if err := d.Sleep(); err != nil {
		t.Fatal(err)
	}
	if err := d.EnergyScan(26, 200); err != nil {
		t.Fatal(err)
	}
	if d.GetState() != radio.StateReceive {
		t.Fatal("active energy scan reported sleeping")
	}
	if d.Transmit(txFrame()) != ErrBusy {
		t.Fatal("transmit during scan")
	}
	key := lock()
	d.scanDone(-90)
	d.scanDone(-20)
	unlock(key)
	if n := next(t, cb, "scan"); n.info.RSSI != -90 {
		t.Fatal(n)
	}
}

func TestDisableSettlesWithoutRestartingHardware(t *testing.T) {
	for _, scan := range []bool{false, true} {
		cb := &callbacks{}
		d, h := setup(t, cb, nil)
		if scan {
			if err := d.EnergyScan(15, 100); err != nil {
				t.Fatal(err)
			}
		} else if err := d.Transmit(txFrame()); err != nil {
			t.Fatal(err)
		}
		before := h.calls
		if err := d.Disable(); err != nil {
			t.Fatal(err)
		}
		if h.calls != before || d.GetState() != radio.StateDisabled {
			t.Fatal("disable restarted receive production")
		}
		if scan {
			next(t, cb, "scan")
		} else {
			next(t, cb, "start")
			if n := next(t, cb, "done"); n.err != ErrAbort {
				t.Fatal(n)
			}
		}
	}
}
func TestEnableFailureAndShutdownFailure(t *testing.T) {
	cb := &callbacks{got: make(chan notice, 8)}
	h := &fakeDevice{initError: errors.New("calibration failed")}
	d := newRadio(h)
	if err := d.SetCallbacks(cb, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Enable(); err != h.initError || d.IsEnabled() {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	<-d.Done()

	cb = &callbacks{}
	d, h = setup(t, cb, nil)
	h.shutdownError = errors.New("DMA not quiescent")
	if err := d.Disable(); err != h.shutdownError {
		t.Fatal(err)
	}
	if err := d.Close(); err != h.shutdownError {
		t.Fatal("lost quiescence error", err)
	}
	h.shutdownError = nil
	key := lock()
	d.shutdownError = nil
	unlock(key) // simulate explicit board reset for cleanup
}

func TestSourceMatching(t *testing.T) {
	d := newRadio(&fakeDevice{})
	shortFrame := []byte{0x63, 0x98, 1, 0xcd, 0xab, 0x34, 0x12, 0x78, 0x56, 4}
	if !d.pending(shortFrame) {
		t.Fatal("disabled source matching must set pending")
	}
	d.EnableSrcMatch(true)
	if d.pending(shortFrame) {
		t.Fatal("empty table matched")
	}
	if err := d.AddSrcMatchShortEntry(0x5678); err != nil {
		t.Fatal(err)
	}
	if !d.pending(shortFrame) {
		t.Fatal("short little-endian address did not match")
	}
	ext := types.ExtAddress{1, 2, 3, 4, 5, 6, 7, 8}
	extFrame := []byte{0x63, 0xd8, 1, 0xcd, 0xab, 0x34, 0x12, 8, 7, 6, 5, 4, 3, 2, 1, 4}
	if err := d.AddSrcMatchExtEntry(ext); err != nil {
		t.Fatal(err)
	}
	if !d.pending(extFrame) {
		t.Fatal("extended wire address did not match")
	}
	for n := 0; n < len(extFrame)-2; n++ {
		if n < 15 && d.pending(extFrame[:n]) {
			t.Fatal("truncated source matched", n)
		}
	}
	d.ClearSrcMatchShortEntries()
	d.ClearSrcMatchExtEntries()
	if d.pending(shortFrame) || d.pending(extFrame) {
		t.Fatal("cleared source table matched")
	}
	for i := 0; i < sourceSize; i++ {
		if err := d.AddSrcMatchShortEntry(types.ShortAddress(i)); err != nil {
			t.Fatal(err)
		}
	}
	if d.AddSrcMatchShortEntry(100) != ErrBusy {
		t.Fatal("unbounded source table")
	}
	if d.AddSrcMatchShortEntry(0) != nil {
		t.Fatal("duplicate entry failed")
	}
	if d.ClearSrcMatchShortEntry(0) != nil || d.ClearSrcMatchShortEntry(0) != ErrInvalidArgument {
		t.Fatal("clear semantics")
	}
}
