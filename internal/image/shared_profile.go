package image

import "fmt"

type storeProfile uint8

const (
	storeSchema1  storeProfile = 1
	storeSharedV2 storeProfile = 2
)

func profileStart(profile storeProfile) (uint64, error) {
	switch profile {
	case storeSchema1:
		return settingsStart, nil
	case storeSharedV2:
		return 0x3d0000, nil
	default:
		return 0, fmt.Errorf("unknown C6 storage profile")
	}
}

var sharedProfileSymbols = map[string]uint64{
	"__c6_store_profile": 1, "__c6_store_schema": 2, "__c6_store_bank_bytes": 98304,
	"__c6_store_header_bytes": 128, "__c6_store_descriptor_bytes": 32, "__c6_store_directory_slots": 128,
	"__c6_store_arena_start": 4224, "__c6_store_retire_offset": 98300,
	"__c6_store_matter_bytes": 65536, "__c6_store_thread_bytes": 4096,
}

func checkProfileSymbols(values map[string]uint64, profile storeProfile) error {
	start, err := profileStart(profile)
	if err != nil {
		return err
	}
	for name, want := range map[string]uint64{
		"__settings_start": flashBase + start, "__settings_end": flashBase + flashSize,
		"__settings_phys_start": start, "__settings_phys_end": flashSize,
	} {
		if values[name] != want {
			return fmt.Errorf("invalid/missing Settings partition symbol %s", name)
		}
	}
	for name, want := range sharedProfileSymbols {
		got, exists := values[name]
		if profile == storeSchema1 {
			if exists {
				return fmt.Errorf("schema1 image contains shared profile symbol %s", name)
			}
		} else if !exists || got != want {
			return fmt.Errorf("invalid/missing shared profile symbol %s", name)
		}
	}
	return nil
}
