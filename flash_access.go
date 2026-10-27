package esp32c6radio

import (
	"time"

	"github.com/hilli/gothread/radio"
)

var flashOwner *Radio

// WithFlashAccess quiesces MAC/DMA with interrupts enabled, then invokes fn.
// Completion callbacks may remain queued: waiting for their consumption from
// Settings.Set on the Instance loop would deadlock. fn must restore XIP before
// returning and must not re-enter this method or change radio lifecycle.
// The native flash bridge, not this Go method, masks interrupts during XIP loss.
func (d *Radio) WithFlashAccess(fn func() error) (result error) {
	if fn == nil {
		return ErrInvalidArgument
	}
	d.flashMu.Lock()
	defer d.flashMu.Unlock()
	key := lock()
	if d.closeRequested || d.closed {
		unlock(key)
		return ErrClosed
	}
	if d.transition || d.scanning || flashOwner != nil || !flashDeviceAvailable(d) {
		unlock(key)
		return ErrBusy
	}
	previous := d.state
	flashOwner = d
	d.flashPaused = true
	d.rfEpoch++
	unlock(key)
	start := time.Now()
	defer func() {
		key := lock()
		d.flashPaused = false
		flashOwner = nil
		d.rfEpoch++
		d.stats.FlashPauses++
		d.stats.FlashPauseMicroseconds += uint64(time.Since(start).Microseconds())
		if (previous == radio.StateReceive || previous == radio.StateTransmit) && !(d.txPending && !d.txEnded) && !d.dev.busy() && d.dev.idle() {
			d.state = radio.StateReceive
			d.dev.receive(d.channel, &d.cfg)
		}
		unlock(key)
	}()
	if previous != radio.StateDisabled {
		deadline := time.Now().Add(300 * time.Millisecond)
		for {
			key := lock()
			pending := d.txPending && !d.txEnded
			busy := d.dev.busy()
			if !pending && !busy {
				d.dev.stop()
			}
			idle := !pending && !busy && d.dev.idle()
			unlock(key)
			if idle {
				break
			}
			if time.Now().After(deadline) {
				return ErrBusy
			}
			time.Sleep(100 * time.Microsecond)
		}
	}
	return fn()
}

func (d *Radio) resumeReceive() {
	if d.state == radio.StateDisabled {
		return
	}
	d.state = radio.StateReceive
	if d.flashPaused {
		d.dev.stop()
	} else {
		d.dev.receive(d.channel, &d.cfg)
	}
}
