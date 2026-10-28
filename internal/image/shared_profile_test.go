package image

import (
	"debug/elf"
	"encoding/binary"
	"testing"
)

func profileFixture(profile storeProfile) map[string]uint64 {
	start, _ := profileStart(profile)
	m := map[string]uint64{"__settings_start": flashBase + start, "__settings_end": flashBase + flashSize, "__settings_phys_start": start, "__settings_phys_end": flashSize}
	if profile == storeSharedV2 {
		for k, v := range sharedProfileSymbols {
			m[k] = v
		}
	}
	return m
}
func TestClosedSharedProfileRejectsCrossLayoutAndPartialSymbols(t *testing.T) {
	legacy, shared := profileFixture(storeSchema1), profileFixture(storeSharedV2)
	if err := checkProfileSymbols(legacy, storeSchema1); err != nil {
		t.Fatal(err)
	}
	if err := checkProfileSymbols(shared, storeSharedV2); err != nil {
		t.Fatal(err)
	}
	if checkProfileSymbols(legacy, storeSharedV2) == nil || checkProfileSymbols(shared, storeSchema1) == nil {
		t.Fatal("cross-profile accepted")
	}
	if _, err := profileStart(99); err == nil {
		t.Fatal("unknown profile")
	}
	for key := range sharedProfileSymbols {
		m := profileFixture(storeSharedV2)
		delete(m, key)
		if checkProfileSymbols(m, storeSharedV2) == nil {
			t.Fatal("missing symbol", key)
		}
		m = profileFixture(storeSharedV2)
		m[key]++
		if checkProfileSymbols(m, storeSharedV2) == nil {
			t.Fatal("wrong symbol", key)
		}
	}
	legacy["__c6_store_profile"] = 0
	if checkProfileSymbols(legacy, storeSchema1) == nil {
		t.Fatal("partial profile marker accepted")
	}
	if flashRangeTo(flashBase+0x3d0000-4, 8, 0x3d0000) == nil {
		t.Fatal("shared reservation overlap")
	}
}
func TestExtractedSRAMProofClosesOverEveryHelper(t *testing.T) {
	code := make([]byte, 8)
	// AUIPC ra,0; JALR ra,ra,+256 reaches another SRAM function.
	binary.LittleEndian.PutUint32(code, 0x00000097)
	binary.LittleEndian.PutUint32(code[4:], 0x100080e7)
	root := elf.Symbol{Name: "c6_flash_mutate", Value: ramBase, Size: 8}
	helper := elf.Symbol{Name: "c6_flash_profile_mutate", Value: ramBase + 256, Size: 8}
	if checkRVControlClosure(code, ramBase, []elf.Symbol{root}) == nil {
		t.Fatal("unchecked SRAM helper accepted")
	}
	if err := checkRVControlClosure(code, ramBase, []elf.Symbol{root, helper}); err != nil {
		t.Fatal(err)
	}
}
