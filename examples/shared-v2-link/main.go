//go:build tinygo && esp32c6 && esp32c6_radio

// LINK-ONLY harness. It never opens a store, formats, transmits or provisions.
package main

import (
	"unsafe"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/esp32c6-radio/settings"
)

var _ [8224 - unsafe.Sizeof(settings.SharedV2{})]byte

func main() {
	println("LINK ONLY; physical NOT_RUN; object bytes:", unsafe.Sizeof(settings.SharedV2{}))
	println("Object size is NOT the internal peak/stack proof.")
	select {}
}

// Exported solely to retain real API/control-flow bodies in the link proof.
// Neither main nor any interrupt invokes this function. Never flash this image.
//
//export c6_shared_v2_link_witness
func linkWitness(op uint32) int32 {
	driver, err := esp32c6radio.New()
	if err != nil {
		return -1
	}
	if op == 100 {
		legacy, err := settings.Open(driver)
		if err != nil {
			return -2
		}
		if err := legacy.Close(); err != nil {
			return -3
		}
		return 0
	}
	if op == 101 {
		if settings.FormatEmpty(driver) != nil {
			return -4
		}
		return 0
	}
	if op == 102 {
		if settings.FormatEmptySharedV2(driver) != nil {
			return -5
		}
		return 0
	}
	s, err := settings.OpenSharedV2(driver)
	if err != nil {
		return -6
	}
	thread, matter := s.ThreadSettings(), s.MatterBlobs()
	switch op {
	case 0:
		err = thread.Set(65535, nil)
	case 1:
		err = thread.Add(65535, nil)
	case 2:
		_, err = thread.Get(65535, 0)
	case 3:
		err = thread.Delete(65535, -1)
	case 4:
		err = thread.Wipe()
	case 5:
		err = matter.Set("link-only", nil)
	case 6:
		_, err = matter.Get("link-only")
	case 7:
		err = matter.Delete("link-only")
	case 8:
		_, err = matter.Keys("")
	case 9:
		err = matter.Close()
	}
	if err != nil {
		return -7
	}
	if s.Close() != nil {
		return -8
	}
	return 0
}
