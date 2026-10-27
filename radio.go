package esp32c6radio

import (
	"errors"
	"sync"
	"time"

	"github.com/hilli/gothread/mac/submac"
	"github.com/hilli/gothread/mac/types"
	"github.com/hilli/gothread/radio"
)

const (
	maxPayload         = radio.FrameMaxSize - radio.FrameFCSSize
	eventCount         = 16
	sourceSize         = 16
	channelMask uint32 = 0x07fff800
)

var (
	ErrUnsupported          = errors.New("esp32c6-radio: unsupported operation or target")
	ErrInvalidArgument      = errors.New("esp32c6-radio: invalid argument")
	ErrBusy                 = errors.New("esp32c6-radio: busy")
	ErrOverflow             = errors.New("esp32c6-radio: receive event storage overflow")
	ErrNotQuiescent         = errors.New("esp32c6-radio: hardware quiescence is unproven; reset required")
	ErrClosed               = errors.New("esp32c6-radio: closed")
	ErrInvalidState         = submac.ErrInvalidState
	ErrChannelAccessFailure = submac.ErrChannelAccessFailure
	ErrNoAck                = submac.ErrNoAck
	ErrAbort                = submac.ErrAbort
)

type configuration struct {
	pan         uint16
	short       types.ShortAddress
	extended    types.ExtAddress
	power       int8
	promiscuous bool
}

// device is the private boundary exercised by host tests. It represents only
// this driver's MAC operations, not a general-purpose peripheral HAL.
type device interface {
	enable(*Radio) error
	disable() error
	stop()
	busy() bool
	idle() bool
	receive(uint8, *configuration)
	transmit(*[33]uint32, uint8, *configuration, bool)
	scan(uint8, uint32, *configuration)
	factoryEUI() [8]byte
	rssi() int8
}

type eventKind uint8

const (
	eventReceive eventKind = iota
	eventStarted
	eventDone
	eventScan
)

type event struct {
	kind     eventKind
	err      error
	frame    radio.Frame
	ack      radio.Frame
	data     [maxPayload]byte
	ackData  [maxPayload]byte
	txInfo   radio.TxInfo
	rxInfo   radio.RxInfo
	ackInfo  radio.RxInfo
	hasAck   bool
	rssi     int8
	dispatch func()
}

// Statistics includes loss that occurs before callbacks can consume a frame.
// Overflow is also reported as HandleReceiveDone(nil, ErrOverflow).
type Statistics struct {
	ReceiveDropped          uint64
	ReceiveInvalid          uint64
	ReceiveUnsupported      uint64
	UnsupportedCalls        uint64
	FlashPauses             uint64
	FlashPauseMicroseconds  uint64
	TransmitAccepted        uint64
	TransmitTerminal        uint64
	TransmitSuccess         uint64
	TransmitNoAck           uint64
	TransmitCCA             uint64
	TransmitAborted         uint64
	SecuredTransmitAttempts uint64
}

// Radio owns one C6 modem and one outstanding transmit attempt. Frames passed
// to callbacks are borrowed until that callback returns. Copy data to retain it.
type Radio struct {
	dev                                    device
	state                                  radio.State
	cfg                                    configuration
	channel                                uint8
	callbacks                              radio.Callbacks
	post                                   func(func())
	bound, closing, closed, closeRequested bool
	transition                             bool
	done                                   chan struct{}
	consumed                               chan struct{}
	events                                 [eventCount]event
	head, tail, count, reserved            uint8
	overflow                               bool
	txPending, txEnded, scanning           bool
	txWords                                [33]uint32
	txData                                 [maxPayload]byte
	txInfo                                 radio.TxInfo
	txFrame                                radio.Frame
	bufferData                             [maxPayload]byte
	bufferInfo                             radio.TxInfo
	buffer                                 radio.Frame
	srcEnabled                             bool
	shorts                                 [sourceSize]types.ShortAddress
	exts                                   [sourceSize]types.ExtAddress
	shortUsed, extUsed                     [sourceSize]bool
	stats                                  Statistics
	deadline                               time.Time
	shutdownError                          error
	rfEpoch                                uint32
	entropyMu                              sync.Mutex
	entropyLastCycle                       uint32
	flashMu                                sync.Mutex
	flashPaused                            bool
}

var _ radio.Radio = (*Radio)(nil)

// New takes exclusive ownership of the C6 radio. Unsupported targets return an
// error rather than providing a successful host/mock radio.
func New() (*Radio, error) {
	dev, err := newDevice()
	if err != nil {
		return nil, err
	}
	return newRadio(dev), nil
}

func newRadio(dev device) *Radio {
	d := &Radio{
		dev: dev, channel: 11,
		cfg:  configuration{pan: 0xffff, short: 0xffff},
		done: make(chan struct{}), consumed: make(chan struct{}, 1),
	}
	d.bufferInfo.TxPower = radio.PowerInvalid
	d.cfg.extended = types.ExtAddress(dev.factoryEUI())
	d.buffer = radio.Frame{PSDU: d.bufferData[:0], Channel: 11, TxInfo: &d.bufferInfo}
	for i := range d.events {
		index := i
		d.events[i].dispatch = func() { d.dispatch(index) }
	}
	return d
}

// SetCallbacks starts the bounded event worker. With an Instance, pass
// inst.SubMac() and inst.PostTask, after construction and before Receive.
// Enable may precede Instance construction to make hardware Entropy available.
// A nil post function delivers on the worker, never in the interrupt handler.
func (d *Radio) SetCallbacks(callbacks radio.Callbacks, post func(func())) error {
	if callbacks == nil {
		return ErrInvalidArgument
	}
	key := lock()
	defer unlock(key)
	if d.closeRequested || d.closed {
		return ErrClosed
	}
	if d.bound || d.transition || d.state != radio.StateDisabled && d.state != radio.StateSleep {
		return ErrInvalidState
	}
	d.callbacks, d.post, d.bound = callbacks, post, true
	go d.run()
	return nil
}

func (d *Radio) Enable() error {
	key := lock()
	if d.closed || d.closeRequested {
		unlock(key)
		return ErrClosed
	}
	if d.transition {
		unlock(key)
		return ErrInvalidState
	}
	if flashOwner != nil {
		unlock(key)
		return ErrBusy
	}
	if d.state != radio.StateDisabled {
		unlock(key)
		return nil
	}
	d.transition = true
	d.rfEpoch++
	unlock(key)
	err := d.dev.enable(d)
	key = lock()
	d.transition = false
	if err == nil {
		d.state = radio.StateSleep
		d.shutdownError = nil
	} else if errors.Is(err, ErrNotQuiescent) {
		d.shutdownError = err
	}
	unlock(key)
	return err
}

// Disable stops producers and settles an accepted transmit with ErrAbort.
// Queued callbacks still drain. It is safe to call from a callback.
func (d *Radio) Disable() error {
	key := lock()
	if d.transition || d.flashPaused {
		unlock(key)
		return ErrBusy
	}
	if d.state == radio.StateDisabled {
		err := d.shutdownError
		unlock(key)
		return err
	}
	d.dev.stop()
	d.rfEpoch++
	d.state = radio.StateDisabled
	if d.txPending && !d.txEnded {
		d.transmitDone(nil, radio.RxInfo{}, ErrAbort)
	}
	if d.scanning {
		d.scanDone(radio.RSSIInvalid)
	}
	d.transition = true
	unlock(key)
	err := d.dev.disable()
	key = lock()
	d.transition = false
	d.shutdownError = err
	unlock(key)
	return err
}

// Close stops event production and starts draining the bounded callback queue.
// It is safe inside a callback. After Close succeeds, wait for Done OUTSIDE the
// stack loop, while it is still running, and BEFORE calling Instance.Stop.
// Disable/Enable is the reversible stop/start operation; Close is terminal.
func (d *Radio) Close() error {
	key := lock()
	if d.closed || d.closing {
		err := d.shutdownError
		unlock(key)
		return err
	}
	if d.transition || d.flashPaused {
		unlock(key)
		return ErrBusy
	}
	d.closeRequested = true
	unlock(key)
	if err := d.Disable(); err != nil {
		return err
	}
	key = lock()
	d.closing = true
	bound := d.bound
	if !bound {
		d.closed = true
		close(d.done)
	}
	unlock(key)
	return nil
}

// Done closes only after Close has quiesced producers and every queued
// callback has returned. Never wait for it from a callback or the stack loop.
func (d *Radio) Done() <-chan struct{} { return d.done }

func (d *Radio) IsEnabled() bool { return d.GetState() != radio.StateDisabled }
func (d *Radio) GetState() radio.State {
	key := lock()
	defer unlock(key)
	return d.state
}

func (d *Radio) Sleep() error {
	key := lock()
	defer unlock(key)
	if d.state == radio.StateDisabled || d.transition || !d.bound {
		return ErrInvalidState
	}
	if d.txPending || d.scanning || d.dev.busy() || d.flashPaused {
		return ErrBusy
	}
	d.dev.stop()
	d.state = radio.StateSleep
	return nil
}

func validChannel(channel uint8) bool { return channel >= 11 && channel <= 26 }

func (d *Radio) Receive(channel uint8) error {
	if !validChannel(channel) {
		return ErrInvalidArgument
	}
	key := lock()
	defer unlock(key)
	if d.state == radio.StateDisabled || d.transition || !d.bound {
		return ErrInvalidState
	}
	if d.txPending || d.scanning || d.flashPaused {
		return ErrBusy
	}
	if d.dev.busy() {
		if d.state == radio.StateReceive && d.channel == channel {
			return nil
		}
		return ErrBusy
	}
	d.channel = channel
	d.dev.receive(channel, &d.cfg)
	d.state = radio.StateReceive
	return nil
}

func (d *Radio) Transmit(frame *radio.Frame) error {
	if frame == nil || !validChannel(frame.Channel) || len(frame.PSDU) < 3 || len(frame.PSDU) > maxPayload {
		return ErrInvalidArgument
	}
	// Initial Thread scope is legacy 2003/2006 frames. Enhanced ACKs, CSL
	// and scheduled transmission are not silently downgraded.
	if frame.PSDU[1]>>4&3 > 1 {
		return ErrUnsupported
	}
	info := radio.TxInfo{TxPower: radio.PowerInvalid}
	if frame.TxInfo != nil {
		info = *frame.TxInfo
	}
	if info.Timestamp != 0 || info.CslPresent {
		return ErrUnsupported
	}
	if frame.PSDU[0]&8 != 0 && !info.IsSecurityProcessed {
		return ErrUnsupported
	}
	if info.RxChannelAfterTx != 0 && !validChannel(info.RxChannelAfterTx) {
		return ErrInvalidArgument
	}
	if info.TxPower != radio.PowerInvalid && !validPower(info.TxPower) {
		return ErrInvalidArgument
	}
	key := lock()
	defer unlock(key)
	if d.state != radio.StateReceive || d.transition || !d.bound {
		return ErrInvalidState
	}
	if d.txPending || d.scanning || d.flashPaused || d.count > eventCount-2 {
		return ErrBusy
	}
	if d.dev.busy() {
		return ErrBusy
	}
	d.txPending, d.txEnded, d.reserved = true, false, 2
	d.stats.TransmitAccepted++
	if frame.PSDU[0]&8 != 0 && info.IsSecurityProcessed {
		d.stats.SecuredTransmitAttempts++
	}
	d.txInfo = info
	d.txFrame = *frame
	d.txFrame.PSDU = d.txData[:len(frame.PSDU)]
	copy(d.txFrame.PSDU, frame.PSDU)
	d.txFrame.TxInfo, d.txFrame.RxInfo = &d.txInfo, nil
	d.channel = frame.Channel
	words := dmaBytes(&d.txWords)
	words[0] = byte(len(frame.PSDU) + 2)
	copy(words[1:], frame.PSDU)
	words[len(frame.PSDU)+1], words[len(frame.PSDU)+2] = 0, 0
	cfg := d.cfg
	if info.TxPower != radio.PowerInvalid {
		cfg.power = info.TxPower
	}
	d.state = radio.StateTransmit
	d.deadline = time.Now().Add(250 * time.Millisecond)
	d.dev.transmit(&d.txWords, frame.Channel, &cfg, info.CsmaCaEnabled)
	d.reserved--
	e := d.push(eventStarted)
	d.copyTX(e)
	return nil
}

func (d *Radio) GetTransmitBuffer() *radio.Frame { return &d.buffer }
func (d *Radio) GetCaps() radio.Caps             { return radio.CapAckTimeout | radio.CapEnergyScan }
func (d *Radio) GetIEEEEui64() [8]byte           { return d.dev.factoryEUI() }
func (d *Radio) GetSupportedChannelMask() uint32 { return channelMask }
func (d *Radio) GetPreferredChannelMask() uint32 { return channelMask }

// ESP-IDF v5.5's OpenThread port baseline; not a measured board sensitivity.
func (d *Radio) GetReceiveSensitivity() int8 { return -120 }

func validPower(power int8) bool {
	return power == 20 || (power >= -15 && power <= 18 && (power+15)%3 == 0)
}

func (d *Radio) GetTransmitPower() (int8, error) {
	key := lock()
	defer unlock(key)
	return d.cfg.power, nil
}
func (d *Radio) SetTransmitPower(power int8) error {
	if !validPower(power) {
		return ErrInvalidArgument
	}
	key := lock()
	defer unlock(key)
	d.cfg.power = power
	d.applyConfig()
	return nil
}
func (d *Radio) SetPanID(pan uint16) {
	key := lock()
	defer unlock(key)
	d.cfg.pan = pan
	d.applyConfig()
}
func (d *Radio) SetShortAddress(addr types.ShortAddress) {
	key := lock()
	defer unlock(key)
	d.cfg.short = addr
	d.applyConfig()
}
func (d *Radio) SetExtendedAddress(addr types.ExtAddress) {
	key := lock()
	defer unlock(key)
	d.cfg.extended = addr
	d.applyConfig()
}
func (d *Radio) SetPromiscuous(enable bool) {
	key := lock()
	defer unlock(key)
	d.cfg.promiscuous = enable
	d.applyConfig()
}
func (d *Radio) GetPromiscuous() bool {
	key := lock()
	defer unlock(key)
	return d.cfg.promiscuous
}
func (d *Radio) GetRSSI() int8 {
	key := lock()
	defer unlock(key)
	if d.state == radio.StateDisabled {
		return radio.RSSIInvalid
	}
	return d.dev.rssi()
}
func (d *Radio) applyConfig() {
	if d.state == radio.StateReceive && !d.txPending && !d.scanning && !d.transition && !d.flashPaused && !d.dev.busy() {
		d.dev.receive(d.channel, &d.cfg)
	}
}

func (d *Radio) EnergyScan(channel uint8, duration uint16) error {
	if !validChannel(channel) || duration == 0 {
		return ErrInvalidArgument
	}
	if uint32(duration)*1000 > 0xffffff {
		return ErrUnsupported
	}
	key := lock()
	defer unlock(key)
	if d.state == radio.StateDisabled || d.transition || !d.bound {
		return ErrInvalidState
	}
	if d.txPending || d.scanning || d.flashPaused || d.count == eventCount || d.dev.busy() {
		return ErrBusy
	}
	d.scanning = true
	d.state = radio.StateReceive
	d.reserved = 1
	d.channel = channel
	d.deadline = time.Now().Add(time.Duration(duration)*time.Millisecond + 250*time.Millisecond)
	d.dev.scan(channel, uint32(duration)*1000, &d.cfg)
	return nil
}

func (d *Radio) Statistics() Statistics {
	key := lock()
	defer unlock(key)
	return d.stats
}

// These void methods are never used by gothread when CapTransmitSec is absent.
// Count misuse instead of pretending that hardware encryption was configured.
func (d *Radio) SetMacKey(uint8, uint8, types.KeyMaterial, types.KeyMaterial, types.KeyMaterial) {
	d.unsupportedSecurity()
}
func (d *Radio) SetMacFrameCounter(uint32)         { d.unsupportedSecurity() }
func (d *Radio) SetMacFrameCounterIfLarger(uint32) { d.unsupportedSecurity() }
func (d *Radio) unsupportedSecurity() {
	key := lock()
	defer unlock(key)
	d.stats.UnsupportedCalls++
}

func (d *Radio) run() {
	for {
		key := lock()
		if !d.closing && (d.scanning || d.txPending && !d.txEnded) && time.Now().After(d.deadline) {
			d.dev.stop()
			if d.scanning {
				d.scanDone(radio.RSSIInvalid)
			} else {
				d.transmitDone(nil, radio.RxInfo{}, ErrAbort)
			}
		}
		if d.count == 0 && d.closing {
			d.closed = true
			unlock(key)
			close(d.done)
			return
		}
		if d.count == 0 {
			unlock(key)
			time.Sleep(time.Millisecond)
			continue
		}
		fn := d.events[d.head].dispatch
		post := d.post
		unlock(key)
		if post != nil {
			post(fn)
		} else {
			fn()
		}
		<-d.consumed
	}
}

func (d *Radio) dispatch(index int) {
	key := lock()
	e := &d.events[index]
	if e.kind == eventDone {
		d.txPending = false
	}
	callbacks := d.callbacks
	unlock(key)
	switch e.kind {
	case eventReceive:
		if e.err != nil {
			callbacks.HandleReceiveDone(nil, e.err)
		} else {
			callbacks.HandleReceiveDone(&e.frame, nil)
		}
	case eventStarted:
		callbacks.HandleTransmitStarted(&e.frame)
	case eventDone:
		var ack *radio.Frame
		if e.hasAck {
			ack = &e.ack
		}
		callbacks.HandleTransmitDone(&e.frame, ack, e.err)
	case eventScan:
		callbacks.HandleEnergyScanDone(e.rssi)
	}
	key = lock()
	d.head = (d.head + 1) % eventCount
	d.count--
	if d.overflow && d.count < eventCount-d.reserved {
		d.overflow = false
		overflow := d.push(eventReceive)
		overflow.err = ErrOverflow
	}
	unlock(key)
	d.consumed <- struct{}{}
}

// All producers below run with interrupts disabled. They neither allocate nor
// wait. TX/scan completion slots were reserved before starting the hardware.
func (d *Radio) push(kind eventKind) *event {
	e := &d.events[d.tail]
	d.tail = (d.tail + 1) % eventCount
	d.count++
	e.kind, e.err, e.hasAck = kind, nil, false
	return e
}

func (d *Radio) copyTX(e *event) {
	e.frame = d.txFrame
	e.frame.PSDU = e.data[:len(d.txFrame.PSDU)]
	copy(e.frame.PSDU, d.txFrame.PSDU)
	e.txInfo = d.txInfo
	e.frame.TxInfo = &e.txInfo
}

func (d *Radio) receiveDone(psdu []byte, channel uint8, info radio.RxInfo) {
	if d.state == radio.StateDisabled || d.closing {
		return
	}
	if len(psdu) < 3 || len(psdu) > maxPayload {
		d.stats.ReceiveInvalid++
		return
	}
	if d.count >= eventCount-d.reserved {
		d.stats.ReceiveDropped++
		d.overflow = true
		return
	}
	e := d.push(eventReceive)
	e.frame = radio.Frame{PSDU: e.data[:len(psdu)], Channel: channel, RxInfo: &e.rxInfo}
	copy(e.frame.PSDU, psdu)
	e.rxInfo = info
}

func (d *Radio) transmitDone(ack []byte, info radio.RxInfo, err error) {
	if !d.txPending || d.txEnded {
		return
	}
	d.txEnded = true
	d.stats.TransmitTerminal++
	switch err {
	case nil:
		d.stats.TransmitSuccess++
	case ErrNoAck:
		d.stats.TransmitNoAck++
	case ErrChannelAccessFailure:
		d.stats.TransmitCCA++
	default:
		d.stats.TransmitAborted++
	}
	d.reserved--
	e := d.push(eventDone)
	d.copyTX(e)
	e.err = err
	if len(ack) >= 3 && len(ack) <= maxPayload {
		e.hasAck = true
		e.ack = radio.Frame{PSDU: e.ackData[:len(ack)], Channel: d.channel, RxInfo: &e.ackInfo}
		copy(e.ack.PSDU, ack)
		e.ackInfo = info
	}
	if d.txInfo.RxChannelAfterTx != 0 {
		d.channel = d.txInfo.RxChannelAfterTx
	}
	d.resumeReceive()
}

func (d *Radio) scanDone(rssi int8) {
	if !d.scanning {
		return
	}
	d.scanning = false
	d.reserved--
	e := d.push(eventScan)
	e.rssi = rssi
	d.resumeReceive()
}
