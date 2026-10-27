// Package image verifies the linked ELF and the exact ESP ROM image layout.
package image

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	flashBase     = 0x42000000
	flashSize     = 4 << 20
	ramBase       = 0x40800000
	ramLimit      = 0x4086e610
	settingsStart = 0x3f8000
)

type Report struct {
	FlashBytes uint64
	StaticRAM  uint64
	HeapBytes  uint64
}

func Check(elfPath, binPath string) (Report, error) {
	var report Report
	f, err := elf.Open(elfPath)
	if err != nil {
		return report, err
	}
	defer f.Close()
	if f.Machine != elf.EM_RISCV || f.Class != elf.ELFCLASS32 {
		return report, fmt.Errorf("expected a 32-bit RISC-V ELF")
	}
	symbols, err := f.Symbols()
	if err != nil {
		return report, err
	}
	values := make(map[string]uint64)
	for _, s := range symbols {
		if s.Section != elf.SHN_UNDEF {
			values[s.Name] = s.Value
		}
	}
	required := []string{"_heap_start", "_heap_end"}
	if _, flashOnly := values["c6_flash_mutate"]; !flashOnly {
		required = append(required, "register_chipv7_phy", "bt_bb_v2_init_cmplx", "phy_printf", "rtc_clk_xtal_freq_get", "phy_param_rom")
	}
	for _, name := range required {
		if _, ok := values[name]; !ok {
			return report, fmt.Errorf("missing linked symbol %s", name)
		}
	}
	for name, want := range map[string]uint64{
		"__settings_start": flashBase + settingsStart, "__settings_end": flashBase + flashSize,
		"__settings_phys_start": settingsStart, "__settings_phys_end": flashSize,
	} {
		if values[name] != want {
			return report, fmt.Errorf("invalid/missing Settings partition symbol %s", name)
		}
	}
	if err := checkFlashBridge(f, symbols); err != nil {
		return report, err
	}
	heapStart, heapEnd := values["_heap_start"], values["_heap_end"]
	if heapStart < ramBase || heapStart >= heapEnd || heapEnd != ramLimit {
		return report, fmt.Errorf("invalid SRAM heap bounds %#x..%#x", heapStart, heapEnd)
	}
	if address, ok := values["phy_param_rom"]; ok && address < heapEnd {
		return report, fmt.Errorf("heap overlaps PHY ROM workspace")
	}
	report.StaticRAM = heapStart - ramBase
	report.HeapBytes = heapEnd - heapStart

	var segments []segment
	for _, name := range []string{".data", ".iram", ".text", ".rodata"} {
		s := f.Section(name)
		if s == nil {
			return report, fmt.Errorf("missing ELF section %s", name)
		}
		if s.Size == 0 {
			continue
		}
		if s.Size%4 != 0 {
			return report, fmt.Errorf("%s is not word-sized", name)
		}
		if name == ".data" || name == ".iram" {
			if s.Addr < ramBase || s.Addr+s.Size > heapStart {
				return report, fmt.Errorf("%s overlaps heap or ROM workspace", name)
			}
		} else if err := flashRange(s.Addr, s.Size); err != nil {
			return report, fmt.Errorf("%s: %w", name, err)
		}
		data, err := s.Data()
		if err != nil {
			return report, err
		}
		segments = append(segments, segment{uint32(s.Addr), data})
	}
	bin, err := os.ReadFile(binPath)
	if err != nil {
		return report, err
	}
	if err := checkBinary(bin, uint32(f.Entry), segments); err != nil {
		return report, err
	}
	report.FlashBytes = uint64(len(bin))
	return report, nil
}

type segment struct {
	address uint32
	data    []byte
}

func checkBinary(bin []byte, entry uint32, expected []segment) error {
	if len(bin) < 24 || len(bin) > settingsStart || bin[0] != 0xe9 {
		return fmt.Errorf("invalid ESP image header or size")
	}

	if int(bin[1]) != len(expected) {
		return fmt.Errorf("unexpected ESP segment count %d", bin[1])
	}
	if binary.LittleEndian.Uint32(bin[4:8]) != entry {
		return fmt.Errorf("ELF and ESP entry points differ")
	}
	if binary.LittleEndian.Uint16(bin[12:14]) != 13 {
		return fmt.Errorf("image does not target ESP32-C6")
	}
	offset := 24
	checksum := byte(0xef)
	for _, want := range expected {
		if offset+8 > len(bin) {
			return fmt.Errorf("truncated segment header")
		}
		address := binary.LittleEndian.Uint32(bin[offset:])
		length := uint64(binary.LittleEndian.Uint32(bin[offset+4:]))
		offset += 8
		if address != want.address || length != uint64(len(want.data)) {
			return fmt.Errorf("ELF and image segment %#x differ", address)
		}
		if length > uint64(len(bin)-offset) {
			return fmt.Errorf("truncated segment payload")
		}
		if address >= flashBase && uint64(address-flashBase) != uint64(offset) {
			return fmt.Errorf("nonidentity flash mapping: address %#x, file offset %#x", address, offset)
		}
		payload := bin[offset : offset+int(length)]
		if !bytes.Equal(payload, want.data) {
			return fmt.Errorf("segment %#x payload differs from ELF", address)
		}
		for _, b := range payload {
			checksum ^= b
		}
		offset += int(length)
	}
	checksumOffset := (offset/16+1)*16 - 1
	if checksumOffset >= len(bin) || bin[checksumOffset] != checksum {
		return fmt.Errorf("invalid ESP image checksum")
	}
	for _, b := range bin[offset:checksumOffset] {
		if b != 0 {
			return fmt.Errorf("nonzero ESP image padding")
		}
	}
	end := checksumOffset + 1
	if bin[23] == 1 {
		hash := sha256.Sum256(bin[:end])
		if len(bin) != end+sha256.Size || !bytes.Equal(bin[end:], hash[:]) {
			return fmt.Errorf("invalid ESP appended SHA256")
		}
	} else if bin[23] != 0 || len(bin) != end {
		return fmt.Errorf("unexpected ESP image trailer")
	}
	return nil
}

func flashRange(address, size uint64) error {
	if address < flashBase || address >= flashBase+settingsStart || size > flashBase+settingsStart-address {
		return fmt.Errorf("firmware overlaps Settings partition or physical flash bound")
	}
	return nil
}
