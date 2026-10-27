package image

import (
	"encoding/binary"
	"testing"
)

func TestSettingsPartitionBounds(t *testing.T) {
	for _, tc := range []struct {
		addr, size uint64
		bad        bool
	}{
		{flashBase, settingsStart, false}, {flashBase + settingsStart - 4, 4, false},
		{flashBase + settingsStart - 4, 8, true}, {flashBase + settingsStart, 4, true},
		{flashBase - 4, 4, true}, {flashBase, 1 << 63, true},
	} {
		if err := flashRange(tc.addr, tc.size); (err != nil) != tc.bad {
			t.Fatal(tc, err)
		}
	}
}
func TestFlashControlProofRejectsXIPAndIndirect(t *testing.T) {
	code := make([]byte, 8)
	// AUIPC ra, 0x1800000; JALR ra, ra: SRAM->XIP is forbidden.
	binary.LittleEndian.PutUint32(code, 0x01800097)
	binary.LittleEndian.PutUint32(code[4:], 0x000080e7)
	if checkRVControl(code, ramBase) == nil {
		t.Fatal("accepted XIP call")
	}
	binary.LittleEndian.PutUint32(code, 0x000080e7)
	if checkRVControl(code[:4], ramBase) == nil {
		t.Fatal("accepted unknown indirect call")
	}
	if err := checkRVControl([]byte{0x82, 0x80}, ramBase); err != nil {
		t.Fatal("rejected return", err)
	}
}
