package image

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"strings"
)

func executableMemory(address uint64) bool {
	return address >= 0x40000000 && address < 0x40050000 || address >= ramBase && address < ramLimit
}

func checkFlashBridge(f *elf.File, symbols []elf.Symbol) error {
	var functions []elf.Symbol
	for _, s := range symbols {
		if strings.HasPrefix(s.Name, "c6_flash_") || strings.HasPrefix(s.Name, "c6_shared_v2_flash_") || s.Name == "ram_range" {
			if s.Section == elf.SHN_UNDEF {
				continue
			}
			if s.Value < ramBase || s.Value+s.Size > ramLimit || s.Size == 0 {
				return fmt.Errorf("%s is not fully in SRAM", s.Name)
			}
			functions = append(functions, s)
		}
	}
	if len(functions) == 0 {
		return nil
	}
	foundEntry := false
	for _, fn := range functions {
		if fn.Name == "c6_flash_mutate" || fn.Name == "c6_shared_v2_flash_mutate" {
			foundEntry = true
		}
	}
	if !foundEntry {
		return nil
	} // a read-only geometry probe may not link mutation
	for _, fn := range functions {
		sec := f.Sections[fn.Section]
		if sec.Name != ".iram" {
			return fmt.Errorf("%s has unexpected section %s", fn.Name, sec.Name)
		}
		data, err := sec.Data()
		if err != nil {
			return err
		}
		code := data[fn.Value-sec.Addr : fn.Value-sec.Addr+fn.Size]
		if err := checkRVControlClosure(code, fn.Value, functions); err != nil {
			return fmt.Errorf("%s: %w", fn.Name, err)
		}
	}
	relocs := false
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_RELA || sec.Info >= uint32(len(f.Sections)) || f.Sections[sec.Info].Name != ".iram" {
			continue
		}
		relocs = true
		data, err := sec.Data()
		if err != nil {
			return err
		}
		for len(data) >= 12 {
			off := uint64(binary.LittleEndian.Uint32(data))
			info := binary.LittleEndian.Uint32(data[4:])
			index := int(info>>8) - 1
			relevant := false
			for _, fn := range functions {
				if off >= fn.Value && off < fn.Value+fn.Size {
					relevant = true
				}
			}
			if relevant && index >= 0 && index < len(symbols) {
				to := symbols[index]
				if to.Value >= flashBase && to.Value < flashBase+flashSize {
					return fmt.Errorf("cache-off relocation targets XIP %s", to.Name)
				}
			}
			data = data[12:]
		}
	}
	if !relocs {
		return fmt.Errorf("flash bridge proof requires --emit-relocs")
	}
	return nil
}

func signed(value uint32, bits uint) int64 { return int64(int32(value<<(32-bits)) >> (32 - bits)) }
func target(pc uint64, offset int64) error {
	address := uint64(int64(pc) + offset)
	if !executableMemory(address) {
		return fmt.Errorf("cache-off control transfer to %#x", address)
	}
	return nil
}

// Decode actual RV32IMC direct control transfers. Unknown indirect transfers
// fail closed; returns are allowed, and AUIPC/JALR call pairs are resolved.
func checkRVControl(code []byte, base uint64) error {
	return checkRVControlClosure(code, base, nil)
}

func closedTarget(pc uint64, offset int64, functions []elf.Symbol) error {
	if err := target(pc, offset); err != nil {
		return err
	}
	address := uint64(int64(pc) + offset)
	if functions == nil || address < ramBase {
		return nil
	}
	for _, fn := range functions {
		if address >= fn.Value && address < fn.Value+fn.Size {
			return nil
		}
	}
	return fmt.Errorf("cache-off transfer to unverified SRAM helper %#x", address)
}

func checkRVControlClosure(code []byte, base uint64, functions []elf.Symbol) error {
	var previous uint32
	var previousPC uint64
	for off := 0; off < len(code); {
		pc := base + uint64(off)
		if off+2 > len(code) {
			return fmt.Errorf("truncated RV instruction")
		}
		half := binary.LittleEndian.Uint16(code[off:])
		if half&3 != 3 {
			funct := half >> 13
			if half&3 == 1 && (funct == 1 || funct == 5) {
				imm := uint32((half>>12&1)<<11 | (half>>11&1)<<4 | (half>>9&3)<<8 | (half>>8&1)<<10 | (half>>7&1)<<6 | (half>>6&1)<<7 | (half>>3&7)<<1 | (half>>2&1)<<5)
				if err := closedTarget(pc, signed(imm, 12), functions); err != nil {
					return err
				}
				if half&3 == 1 && (funct == 6 || funct == 7) {
					imm := uint32((half>>12&1)<<8 | (half>>10&3)<<3 | (half>>5&3)<<6 | (half>>3&3)<<1 | (half>>2&1)<<5)
					if err := closedTarget(pc, signed(imm, 9), functions); err != nil {
						return err
					}
				}
			}
			if half&0xf07f == 0x8002 || half&0xf07f == 0x9002 {
				rs := half >> 7 & 31
				if rs != 1 || half&0x1000 != 0 {
					return fmt.Errorf("unresolved compressed indirect branch at %#x", pc)
				}
			}
			previous = 0
			off += 2
			continue
		}
		if off+4 > len(code) {
			return fmt.Errorf("truncated RV32 instruction")
		}
		word := binary.LittleEndian.Uint32(code[off:])
		op := word & 0x7f
		if op == 0x6f {
			imm := (word>>31)<<20 | (word>>12&255)<<12 | (word>>20&1)<<11 | (word>>21&1023)<<1
			if err := closedTarget(pc, signed(imm, 21), functions); err != nil {
				return err
			}
		}
		if op == 0x63 {
			imm := (word>>31)<<12 | (word>>7&1)<<11 | (word>>25&63)<<5 | (word>>8&15)<<1
			if err := closedTarget(pc, signed(imm, 13), functions); err != nil {
				return err
			}
		}
		if op == 0x67 {
			rd, rs := word>>7&31, word>>15&31
			if !(rd == 0 && rs == 1 && word>>20 == 0) {
				if previous&0x7f != 0x17 || previous>>7&31 != rs {
					return fmt.Errorf("unresolved JALR at %#x", pc)
				}
				high := int64(int32(previous & 0xfffff000))
				low := signed(word>>20, 12)
				if err := closedTarget(previousPC, high+low, functions); err != nil {
					return err
				}
			}
		}
		previous, previousPC = word, pc
		off += 4
	}
	return nil
}
