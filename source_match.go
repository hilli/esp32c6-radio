package esp32c6radio

import (
	"encoding/binary"

	"github.com/hilli/gothread/mac/types"
)

func (d *Radio) EnableSrcMatch(enable bool) {
	key := lock()
	defer unlock(key)
	d.srcEnabled = enable
}
func (d *Radio) AddSrcMatchShortEntry(addr types.ShortAddress) error {
	key := lock()
	defer unlock(key)
	free := -1
	for i := range d.shorts {
		if d.shortUsed[i] && d.shorts[i] == addr {
			return nil
		}
		if !d.shortUsed[i] {
			free = i
		}
	}
	if free < 0 {
		return ErrBusy
	}
	d.shorts[free], d.shortUsed[free] = addr, true
	return nil
}
func (d *Radio) AddSrcMatchExtEntry(addr types.ExtAddress) error {
	key := lock()
	defer unlock(key)
	free := -1
	for i := range d.exts {
		if d.extUsed[i] && d.exts[i] == addr {
			return nil
		}
		if !d.extUsed[i] {
			free = i
		}
	}
	if free < 0 {
		return ErrBusy
	}
	d.exts[free], d.extUsed[free] = addr, true
	return nil
}
func (d *Radio) ClearSrcMatchShortEntry(addr types.ShortAddress) error {
	key := lock()
	defer unlock(key)
	for i := range d.shorts {
		if d.shortUsed[i] && d.shorts[i] == addr {
			d.shortUsed[i] = false
			return nil
		}
	}
	return ErrInvalidArgument
}
func (d *Radio) ClearSrcMatchExtEntry(addr types.ExtAddress) error {
	key := lock()
	defer unlock(key)
	for i := range d.exts {
		if d.extUsed[i] && d.exts[i] == addr {
			d.extUsed[i] = false
			return nil
		}
	}
	return ErrInvalidArgument
}
func (d *Radio) ClearSrcMatchShortEntries() {
	key := lock()
	defer unlock(key)
	d.shortUsed = [sourceSize]bool{}
}
func (d *Radio) ClearSrcMatchExtEntries() {
	key := lock()
	defer unlock(key)
	d.extUsed = [sourceSize]bool{}
}

// pending is called in the RX-done interrupt, before hardware ACK turnaround.
// With source matching disabled, Thread's data-poll ACKs always indicate pending.
func (d *Radio) pending(psdu []byte) bool {
	if !d.srcEnabled {
		return true
	}
	if len(psdu) < 3 {
		return false
	}
	fcf := binary.LittleEndian.Uint16(psdu)
	if fcf>>12&3 > 1 {
		return false
	}
	destMode, sourceMode := fcf>>10&3, fcf>>14&3
	offset := 3
	if destMode != 0 {
		if destMode == 1 {
			return false
		}
		offset += 2
		if destMode == 2 {
			offset += 2
		} else {
			offset += 8
		}
	}
	if sourceMode == 0 || sourceMode == 1 {
		return false
	}
	if destMode == 0 || fcf&(1<<6) == 0 {
		offset += 2
	}
	if sourceMode == 2 {
		if offset+2 > len(psdu) {
			return false
		}
		addr := types.ShortAddress(binary.LittleEndian.Uint16(psdu[offset:]))
		for i := range d.shorts {
			if d.shortUsed[i] && d.shorts[i] == addr {
				return true
			}
		}
	} else {
		if offset+8 > len(psdu) {
			return false
		}
		var addr types.ExtAddress
		for i := range addr {
			addr[i] = psdu[offset+7-i]
		}
		for i := range d.exts {
			if d.extUsed[i] && d.exts[i] == addr {
				return true
			}
		}
	}
	return false
}
