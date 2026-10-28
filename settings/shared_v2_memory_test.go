package settings

import (
	"bytes"
	"testing"
)

func TestSharedV2HostOperationAllocationDeltas(t *testing.T) {
	s, _ := sharedFixture(t)
	matter := s.MatterBlobs()
	input := bytes.Repeat([]byte{0x71}, 4096)
	if err := matter.Set("node/blob", input); err != nil {
		t.Fatal(err)
	}
	// Input, NOR simulator and owner were allocated before the measurement.
	// Target native closure/allocator/stack evidence is a separate acceptance gate.
	sets := testing.AllocsPerRun(4, func() {
		if err := matter.Set("node/blob", input); err != nil {
			panic(err)
		}
	})
	gets := testing.AllocsPerRun(4, func() {
		p, err := matter.Get("node/blob")
		if err != nil || len(p) != len(input) {
			panic("get")
		}
	})
	keys := testing.AllocsPerRun(4, func() {
		p, err := matter.Keys("node/")
		if err != nil || len(p) != 1 {
			panic("keys")
		}
	})
	t.Logf("host allocations/op Set=%g Get=%g Keys=%g; Get/Keys outputs are separately accounted", sets, gets, keys)
	if sets != 0 {
		t.Fatal("internal per-Set allocation", sets)
	}
	if gets != 1 {
		t.Fatal("Get allocated more than its exact caller output", gets)
	}
	if keys != 2 {
		t.Fatal("Keys allocated more than its slice and one copied output string", keys)
	}
}

func TestSharedV2HostCompactionAllocationDelta(t *testing.T) {
	s, f := sharedFixture(t)
	data := bytes.Repeat([]byte{0x79}, 4096)
	if err := s.MatterBlobs().Set("blob", data); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(2, func() {
		f.data[int(s.active)*SharedV2BankSize+int(s.banks[s.active].tail)+8] = 0
		if err := s.MatterBlobs().Set("blob", data); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatal("compaction copied/allocated internal state", allocations)
	}
}
