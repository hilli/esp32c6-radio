package esp32c6radio

import (
	"time"
	"unsafe"

	"github.com/hilli/gothread/radio"
)

const (
	macBase         = uintptr(0x600a3000)
	cmd             = 0x00
	conf            = 0x04
	eventEnable     = 0x60
	eventStatus     = 0x64
	rxAbortEnable   = 0x68
	pendingConfig   = 0x6c
	txAbortEnable   = 0x78
	rxStatus        = 0x80
	txStatus        = 0x84
	timer0Threshold = 0xa8
	rxDMA           = 0xe0
	txDMA           = 0xd0
	txDone          = 1 << 0
	rxDone          = 1 << 1
	ackTXDone       = 1 << 2
	ackRXDone       = 1 << 3
	rxAbort         = 1 << 4
	txAbort         = 1 << 5
	edDone          = 1 << 6
	timer0          = 1 << 8
	txSFD           = 1 << 11
	rxSFD           = 1 << 12
	macEvents       = txDone | rxDone | ackTXDone | ackRXDone | rxAbort | txAbort | edDone | timer0 | txSFD | rxSFD
)

type c6Device struct {
	owner                *Radio
	rxWords              [33]uint32
	rxChannel            uint8
	lastRSSI             int8
	rxTimestamp          uint64
	awaitAck, sendingAck bool
	promiscuous          bool
}

var clockEpoch = time.Now()

// These fixed C6 register accesses are the private hardware boundary used by
// the IRQ state-machine tests. Native reads/writes use TinyGo volatile.
type macRegister uintptr

func macReg(offset uintptr) macRegister     { return macRegister(offset) }
func (r macRegister) Get() uint32           { return readMAC(uintptr(r)) }
func (r macRegister) Set(value uint32)      { writeMAC(uintptr(r), value) }
func (r macRegister) SetBits(mask uint32)   { r.Set(r.Get() | mask) }
func (r macRegister) ClearBits(mask uint32) { r.Set(r.Get() &^ mask) }

func clockUS() uint64 { return uint64(time.Since(clockEpoch).Microseconds()) }

func (h *c6Device) stop() {
	macReg(cmd).Set(0x45)
	macReg(cmd).Set(0x4d)
	macReg(eventStatus).Set(0x1fff)
	h.awaitAck, h.sendingAck = false, false
	h.rxTimestamp = 0
}

func (h *c6Device) busy() bool {
	return h.sendingAck || macReg(rxStatus).Get()>>16&7 > 1 || macReg(0x88).Get()&(1<<8) != 0
}

func powerIndex(power int8) uint32 {
	if power == 20 {
		return 15
	}
	return uint32((int(power)+15)/3 + 3)
}

func (h *c6Device) configure(channel uint8, cfg *configuration) {
	h.rxChannel = channel
	h.promiscuous = cfg.promiscuous
	c := uint32(1 | 1<<3 | 1<<12 | 1<<28) // legacy ACKs, Thread 1.2+ pending policy, PAN 0
	if cfg.promiscuous {
		c |= 1 << 7
		c &^= 1 // promiscuous reception must not acknowledge unrelated PANs
	}
	const configMask = 1 | 1<<1 | 1<<3 | 1<<6 | 1<<7 | 1<<12 | 0xf<<28
	macReg(conf).Set(macReg(conf).Get()&^configMask | c)
	macReg(0x08).Set(uint32(cfg.short))
	macReg(0x0c).Set(uint32(cfg.pan))
	var low, high uint32
	for i := 0; i < 4; i++ {
		low |= uint32(cfg.extended[7-i]) << (8 * i)
		high |= uint32(cfg.extended[3-i]) << (8 * i)
	}
	macReg(0x10).Set(low)
	macReg(0x14).Set(high)
	macReg(0x48).Set(uint32(channel-11)*5 + 3)
	macReg(0x4c).Set(powerIndex(cfg.power))
	const edMask = 0xff | 3<<11 | 1<<13 | 3<<14
	macReg(0x54).Set(macReg(0x54).Get()&^edMask | 0x40b5) // energy CCA, maximum ED sample, -75 dBm
	macReg(0x128).Set(0)                                  // never enable hardware MAC encryption
	macReg(0x6c).ClearBits(1)                             // retain the hardware pending-decision timeout
	macReg(0xe4).SetBits(1 << 24)                         // append LQI in place of the second FCS byte
	macReg(0xe4).ClearBits(1 << 25)
}

func (h *c6Device) armRXBuffer() {
	macReg(rxDMA).Set(uint32(uintptr(unsafe.Pointer(&h.rxWords))))
}

func (h *c6Device) receive(channel uint8, cfg *configuration) {
	h.stop()
	h.configure(channel, cfg)
	h.armRXBuffer()
	dmaFence()
	macReg(cmd).Set(0x42)
}

func (h *c6Device) transmit(words *[33]uint32, channel uint8, cfg *configuration, cca bool) {
	h.stop()
	h.configure(channel, cfg)
	h.armRXBuffer()
	macReg(txDMA).Set(uint32(uintptr(unsafe.Pointer(words))))
	dmaFence()
	if cca {
		macReg(0x50).Set(8) // ESP-IDF CCA_DETECTION_TIME, microseconds
		macReg(cmd).Set(0x43)
	} else {
		macReg(cmd).Set(0x41)
	}
}

func (h *c6Device) scan(channel uint8, duration uint32, cfg *configuration) {
	h.stop()
	h.configure(channel, cfg)
	macReg(0x50).Set(duration)
	macReg(cmd).Set(0x44)
}

func (h *c6Device) factoryEUI() [8]byte {
	low, high := readFactoryMAC()
	return factoryEUI(low, high)
}

func factoryEUI(low, high uint32) [8]byte {
	return [8]byte{
		byte(high >> 8), byte(high), byte(low >> 24), byte(high >> 24),
		byte(high >> 16), byte(low >> 16), byte(low >> 8), byte(low),
	}
}

// The C6 ED RSS field is the most recent hardware energy sample, not a claim
// about instantaneous ambient RSSI. Hardware energy scanning is advertised.
func (h *c6Device) rssi() int8 { return h.lastRSSI }

func (h *c6Device) received() ([]byte, radio.RxInfo, bool) {
	dmaFence()
	raw := dmaBytes(&h.rxWords)
	length := int(raw[0] & 0x7f)
	if length < 5 || length > 127 {
		return nil, radio.RxInfo{}, false
	}
	h.lastRSSI = int8(raw[length-1])
	info := radio.RxInfo{
		RSSI: h.lastRSSI, LQI: raw[length], Timestamp: h.rxTimestamp,
	}
	return raw[1 : length-1], info, true
}

func (h *c6Device) irq(events uint32) {
	key := lock()
	defer unlock(key)
	d := h.owner
	if d == nil || !d.bound || d.state == radio.StateDisabled || d.state == radio.StateSleep && !d.scanning {
		return
	}
	wasTransmitting := d.txPending && !d.txEnded
	if events&rxSFD != 0 {
		h.rxTimestamp = clockUS()
	}
	if events&txSFD != 0 && d.txPending && !d.txEnded {
		d.txInfo.Timestamp = clockUS()
	}
	if events&ackRXDone != 0 && d.txPending && !d.txEnded {
		psdu, info, valid := h.received()
		if valid && len(psdu) >= 3 && psdu[0]&7 == 2 && psdu[2] == d.txFrame.PSDU[2] {
			info.AckedWithFramePending = psdu[0]&0x10 != 0
			d.transmitDone(psdu, info, nil)
		} else {
			d.transmitDone(nil, radio.RxInfo{}, ErrNoAck)
		}
	}
	if events&txAbort != 0 && d.txPending && !d.txEnded {
		reason := macReg(txStatus).Get() >> 4 & 31
		var err error = ErrAbort
		if reason == 24 || reason == 25 {
			err = ErrChannelAccessFailure
		}
		if reason >= 2 && reason <= 16 {
			err = ErrNoAck
		}
		if reason == 19 {
			err = ErrUnsupported
		}
		d.transmitDone(nil, radio.RxInfo{}, err)
	}
	if events&txDone != 0 && d.txPending && !d.txEnded {
		if d.txFrame.PSDU[0]&0x20 == 0 {
			d.transmitDone(nil, radio.RxInfo{}, nil)
		} else {
			h.awaitAck = true
			macReg(timer0Threshold).Set(16000)
			macReg(cmd).Set(0x4c)
		}
	}
	if events&timer0 != 0 && h.awaitAck && d.txPending && !d.txEnded {
		d.transmitDone(nil, radio.RxInfo{}, ErrNoAck)
	}
	if wasTransmitting && d.txEnded {
		return
	}
	if events&rxAbort != 0 {
		reason := macReg(rxStatus).Get() >> 4 & 31
		switch {
		case reason == 1 || reason == 17 || reason == 25:
			// STOP outcomes belong to the operation we deliberately retired.
		case d.scanning:
			d.scanDone(radio.RSSIInvalid)
			return
		case !h.awaitAck && !(d.txPending && !d.txEnded):
			d.stats.ReceiveInvalid++
			h.sendingAck = false
			h.receive(d.channel, &d.cfg)
			return
		}
	}
	if events&rxDone != 0 && !h.awaitAck && d.state == radio.StateReceive && !d.scanning {
		psdu, _, valid := h.received()
		if !valid {
			d.stats.ReceiveInvalid++
			h.receive(d.channel, &d.cfg)
		} else if psdu[1]>>4&3 > 1 {
			d.stats.ReceiveUnsupported++
			h.receive(d.channel, &d.cfg)
		} else if psdu[0]&0x20 != 0 && !h.promiscuous {
			h.sendingAck = true
			if d.pending(psdu) {
				macReg(pendingConfig).SetBits(1)
			} else {
				macReg(pendingConfig).ClearBits(1)
			}
		} else {
			h.deliverRX()
		}
	}
	if events&ackTXDone != 0 && h.sendingAck {
		h.sendingAck = false
		h.deliverRX()
	}
	if events&edDone != 0 && d.scanning {
		h.lastRSSI = int8(macReg(0x54).Get() >> 16)
		d.scanDone(h.lastRSSI)
	}
}

func (h *c6Device) deliverRX() {
	psdu, info, valid := h.received()
	if valid {
		info.AckedWithFramePending = macReg(pendingConfig).Get()&1 != 0
		h.owner.receiveDone(psdu, h.rxChannel, info)
	} else {
		h.owner.stats.ReceiveInvalid++
	}
	h.receive(h.owner.channel, &h.owner.cfg)
}
