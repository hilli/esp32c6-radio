//go:build tinygo && esp32c6 && esp32c6_radio

package esp32c6radio

import (
	"device/esp"
	"device/riscv"
	"errors"
	"runtime/volatile"
	"unsafe"

	"github.com/hilli/esp32c6-radio/internal/chip"
	"github.com/hilli/gothread/radio"
)

var activeDevice *c6Device

func readMAC(offset uintptr) uint32 {
	return (*volatile.Register32)(unsafe.Pointer(macBase + offset)).Get()
}
func writeMAC(offset uintptr, value uint32) {
	(*volatile.Register32)(unsafe.Pointer(macBase + offset)).Set(value)
}
func dmaFence() { riscv.Asm("fence rw, rw") }
func readFactoryMAC() (uint32, uint32) {
	return esp.EFUSE.RD_MAC_SPI_SYS_0.Get(), esp.EFUSE.RD_MAC_SPI_SYS_1.Get()
}

func newDevice() (device, error) {
	return &c6Device{lastRSSI: radio.RSSIInvalid}, nil
}

func (h *c6Device) enable(owner *Radio) error {
	key := lock()
	if activeDevice != nil {
		unlock(key)
		return ErrBusy
	}
	activeDevice = h
	unlock(key)
	h.owner = owner
	h.lastRSSI = radio.RSSIInvalid
	_, err := chip.Init()
	if err == nil {
		err = chip.ProbeIRQ()
		if err != nil {
			if shutdownError := chip.Shutdown(); shutdownError != nil {
				return errors.Join(ErrNotQuiescent, shutdownError) // retain DMA buffers until reset
			}
		}
	}
	if err != nil {
		key = lock()
		activeDevice = nil
		unlock(key)
		return err
	}
	chip.SetHandler(h.irq)
	key = lock()
	esp.LP_PERI.SetCLK_EN_RNG_CK_EN(1)
	esp.LP_CLKRST.SetLP_CLK_PO_EN_RNG_OEN(1)
	macReg(eventStatus).Set(0x1fff)
	macReg(eventEnable).Set(macEvents)
	macReg(txAbortEnable).Set(0x7fffffff)
	macReg(rxAbortEnable).Set(0x7fffffff)
	macReg(0x70).Set(0x11) // no Wi-Fi/BLE coexistence arbitration
	unlock(key)
	eui := h.factoryEUI()
	if eui == [8]byte{} || eui == [8]byte{255, 255, 255, 255, 255, 255, 255, 255} || eui[0]&1 != 0 {
		if err := h.disable(); err != nil {
			return err
		}
		return ErrInvalidArgument
	}
	return nil
}

func (h *c6Device) disable() error {
	if err := chip.Shutdown(); err != nil {
		return errors.Join(ErrNotQuiescent, err)
	}
	key := lock()
	activeDevice = nil
	h.owner = nil
	unlock(key)
	return nil
}
