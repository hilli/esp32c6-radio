//go:build tinygo && esp32c6 && esp32c6_radio

package settings

/*
#cgo CFLAGS: -Wall -Wextra -Werror
#include "flash_bridge.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/gothread/radio"
)

var lease sync.Mutex
var owned bool

type c6Medium struct {
	radio   *esp32c6radio.Radio
	scratch [64]uint32
}

func acquire(driver *esp32c6radio.Radio) (*c6Medium, error) {
	if driver == nil {
		return nil, ErrOwner
	}
	lease.Lock()
	defer lease.Unlock()
	if owned {
		return nil, ErrOwner
	}
	if status := int(C.c6_flash_ready()); status != 0 {
		return nil, fmt.Errorf("%w: ROM geometry/guard status %d", ErrNativeUnavailable, status)
	}
	owned = true
	return &c6Medium{radio: driver}, nil
}

// Open never formats or recovers by erasing. This is a source/ELF-validated
// experimental backend, not evidence that physical durability has passed.
func Open(driver *esp32c6radio.Radio) (*Store, error) {
	dev, err := acquire(driver)
	if err != nil {
		return nil, err
	}
	s, err := open(dev, driver.GetIEEEEui64())
	if err != nil {
		dev.release()
	}
	return s, err
}

// FormatEmpty is an explicit first-provisioning operation: radio disabled,
// entire reserved partition erased, correct board identity, no existing state.
func FormatEmpty(driver *esp32c6radio.Radio) error {
	dev, err := acquire(driver)
	if err != nil {
		return err
	}
	defer dev.release()
	return formatEmpty(dev, driver.GetIEEEEui64())
}

func (f *c6Medium) read(p []byte, off int) error {
	if off < 0 || len(p) > PartitionSize || off > PartitionSize-len(p) {
		return ErrCorrupt
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(0x42000000+PartitionStart+off))), len(p))
	copy(p, data)
	return nil
}
func (f *c6Medium) program(p []byte, off int) error {
	if off < 0 || off%4 != 0 || len(p)%4 != 0 || len(p) > PartitionSize || off > PartitionSize-len(p) {
		return ErrCorrupt
	}
	for len(p) > 0 {
		count := min(256-(off&255), len(p))
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&f.scratch)), 256)
		copy(bytes, p[:count])
		status := int(C.c6_flash_mutate(1, C.uint32_t(PartitionStart+off), (*C.uint32_t)(unsafe.Pointer(&f.scratch)), C.uint32_t(count)))
		if status != 0 {
			return fmt.Errorf("settings: ROM program status %d", status)
		}
		off += count
		p = p[count:]
	}
	return nil
}
func (f *c6Medium) eraseBank(bank int) error {
	if bank < 0 || bank > 1 {
		return ErrCorrupt
	}
	for i := 0; i < BankSize; i += SectorSize {
		status := int(C.c6_flash_mutate(2, C.uint32_t(PartitionStart+bank*BankSize+i), nil, SectorSize))
		if status != 0 {
			return fmt.Errorf("settings: ROM erase status %d", status)
		}
	}
	return nil
}
func (f *c6Medium) transaction(fn func() error) error { return f.radio.WithFlashAccess(fn) }
func (f *c6Medium) resetAllowed() error {
	if f.radio.GetState() != radio.StateDisabled {
		return ErrOfflineRequired
	}
	return nil
}
func (f *c6Medium) release() { lease.Lock(); owned = false; lease.Unlock() }
