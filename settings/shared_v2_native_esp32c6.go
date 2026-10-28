//go:build tinygo && esp32c6 && esp32c6_radio

package settings

/*
#cgo CFLAGS: -Wall -Wextra -Werror
#include "flash_bridge.h"
*/
import "C"

import (
	"unsafe"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/gothread/radio"
)

type sharedC6Medium struct{ radio *esp32c6radio.Radio }

func acquireSharedV2(driver *esp32c6radio.Radio) (*sharedC6Medium, error) {
	if driver == nil {
		return nil, ErrOwner
	}
	lease.Lock()
	defer lease.Unlock()
	if owned {
		return nil, ErrOwner
	}
	if C.c6_shared_v2_flash_ready() != 0 {
		return nil, ErrSharedProfile
	}
	owned = true
	return &sharedC6Medium{driver}, nil
}

// OpenSharedV2 requires the explicit shared-v2 target. It never formats,
// migrates, erases, or acquires a second native lease.
func OpenSharedV2(driver *esp32c6radio.Radio) (*SharedV2, error) {
	dev, err := acquireSharedV2(driver)
	if err != nil {
		return nil, err
	}
	s, err := openSharedV2(dev, driver.GetIEEEEui64())
	if err != nil {
		dev.release()
	}
	return s, err
}

// FormatEmptySharedV2 is explicit lab provisioning only: disabled radio and
// every byte in the complete v2 partition already erased. It performs no erase.
func FormatEmptySharedV2(driver *esp32c6radio.Radio) error {
	dev, err := acquireSharedV2(driver)
	if err != nil {
		return err
	}
	defer dev.release()
	return formatSharedV2(dev, driver.GetIEEEEui64())
}

func (d *sharedC6Medium) read(p []byte, off int) error {
	if off < 0 || len(p) > SharedV2PartitionSize || off > SharedV2PartitionSize-len(p) {
		return ErrCorrupt
	}
	mapped := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(0x42000000+SharedV2PartitionStart+off))), len(p))
	copy(p, mapped)
	return nil
}
func (d *sharedC6Medium) program(p []byte, off int) error {
	if off < 0 || off%4 != 0 || len(p) == 0 || len(p) > 256 || len(p)%4 != 0 ||
		off > SharedV2PartitionSize-len(p) || (off&255)+len(p) > 256 {
		return ErrCorrupt
	}
	if C.c6_shared_v2_flash_mutate(1, C.uint32_t(SharedV2PartitionStart+off), (*C.uint32_t)(unsafe.Pointer(&p[0])), C.uint32_t(len(p))) != 0 {
		return ErrAmbiguous
	}
	return nil
}
func (d *sharedC6Medium) eraseSector(off int) error {
	if off < 0 || off%SectorSize != 0 || off > SharedV2PartitionSize-SectorSize {
		return ErrCorrupt
	}
	if C.c6_shared_v2_flash_mutate(2, C.uint32_t(SharedV2PartitionStart+off), nil, SectorSize) != 0 {
		return ErrAmbiguous
	}
	return nil
}
func (d *sharedC6Medium) transaction(fn func() error) error { return d.radio.WithFlashAccess(fn) }
func (d *sharedC6Medium) closeAllowed() error {
	if d.radio.GetState() != radio.StateDisabled {
		return ErrOfflineRequired
	}
	return nil
}
func (d *sharedC6Medium) release() { lease.Lock(); owned = false; lease.Unlock() }
