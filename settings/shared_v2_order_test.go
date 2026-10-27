package settings

import (
	"bytes"
	"testing"
)

func TestSharedV2UncommittedPayloadCannotPoisonAcknowledgedState(t *testing.T) {
	s, f := sharedFixture(t)
	if err := s.MatterBlobs().Set("value", []byte("acknowledged")); err != nil {
		t.Fatal(err)
	}
	clean := f.reboot()
	next := bytes.Repeat([]byte{0x73}, 517)
	candidate, err := openSharedV2(clean, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.MatterBlobs().Set("value", next); err != nil {
		t.Fatal(err)
	}
	// Before the independent final commit, every wholly uncommitted partial
	// payload must leave the acknowledged value usable, not a committed CRC error.
	for cut := 1; cut < clean.steps; cut++ {
		g := f.reboot()
		st, err := openSharedV2(g, owner)
		if err != nil {
			t.Fatal(err)
		}
		g.cut, g.mode = cut, "before"
		if err := st.MatterBlobs().Set("value", next); err == nil {
			t.Fatal(cut)
		}
		st, err = openSharedV2(g.reboot(), owner)
		if err != nil {
			t.Fatal("commit preceded payload/CRC", cut, err)
		}
		requireSharedBlob(t, st, "value", []byte("acknowledged"))
	}
}
