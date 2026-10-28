package settings

import (
	"bytes"
	"testing"
)

func TestSharedV2EveryInitialFormatWordAndTornBankCommit(t *testing.T) {
	clean := newSharedNOR()
	if err := formatSharedV2(clean, owner); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"before", "after", "torn"} {
		for cut := 1; cut <= clean.steps; cut++ {
			f := newSharedNOR()
			f.cut, f.mode = cut, mode
			if err := formatSharedV2(f, owner); err == nil {
				t.Fatal("format failure acknowledged", mode, cut)
			}
			_, err := openSharedV2(f.reboot(), owner)
			if err != nil && err != ErrUnformatted && err != ErrCorrupt && err != ErrAmbiguous {
				t.Fatal(err)
			}
			if f.erases != 0 {
				t.Fatal("startup format erased storage")
			}
		}
	}
}

func TestSharedV2EveryMaximumBlobWordAndPageBoundary(t *testing.T) {
	s, f := sharedFixture(t)
	if err := s.MatterBlobs().Set("blob", []byte("last acknowledged")); err != nil {
		t.Fatal(err)
	}
	clean := f.reboot()
	st, err := openSharedV2(clean, owner)
	if err != nil {
		t.Fatal(err)
	}
	next := bytes.Repeat([]byte{0x69}, 16384)
	if err := st.MatterBlobs().Set("blob", next); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"before", "after", "torn"} {
		for cut := 1; cut <= clean.steps; cut++ {
			g := f.reboot()
			candidate, err := openSharedV2(g, owner)
			if err != nil {
				t.Fatal(err)
			}
			g.cut, g.mode = cut, mode
			if err := candidate.MatterBlobs().Set("blob", next); err == nil {
				t.Fatal("partial maximum blob acknowledged", mode, cut)
			}
			recovered, err := openSharedV2(g.reboot(), owner)
			if err != nil {
				if err != ErrAmbiguous && err != ErrCorrupt {
					t.Fatal(mode, cut, err)
				}
				continue
			}
			p, err := recovered.MatterBlobs().Get("blob")
			if err != nil || (!bytes.Equal(p, []byte("last acknowledged")) && !bytes.Equal(p, next)) {
				t.Fatal(mode, cut, err)
			}
		}
	}
}

func TestSharedV2WholeListAndTombstoneInterruption(t *testing.T) {
	s, f := sharedFixture(t)
	for _, p := range [][]byte{{1}, {2}, {3}} {
		if err := s.ThreadSettings().Add(65535, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MatterBlobs().Set("untouched", []byte("matter")); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{1, -1} {
		clean := f.reboot()
		st, err := openSharedV2(clean, owner)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ThreadSettings().Delete(65535, index); err != nil {
			t.Fatal(err)
		}
		for cut := 1; cut <= clean.steps; cut++ {
			g := f.reboot()
			candidate, err := openSharedV2(g, owner)
			if err != nil {
				t.Fatal(err)
			}
			g.cut, g.mode = cut, "torn"
			if err := candidate.ThreadSettings().Delete(65535, index); err == nil {
				t.Fatal("torn list acknowledged")
			}
			recovered, err := openSharedV2(g.reboot(), owner)
			if err != nil {
				if err != ErrAmbiguous && err != ErrCorrupt {
					t.Fatal(err)
				}
				continue
			}
			requireSharedBlob(t, recovered, "untouched", []byte("matter"))
			var values []byte
			for i := 0; i < 3; i++ {
				p, err := recovered.ThreadSettings().Get(65535, i)
				if err != nil {
					break
				}
				values = append(values, p...)
			}
			old := bytes.Equal(values, []byte{1, 2, 3})
			if !old && index == 1 && !bytes.Equal(values, []byte{1, 3}) {
				t.Fatal("partial list", values)
			}
			if !old && index == -1 && len(values) != 0 {
				t.Fatal("partial tombstone", values)
			}
		}
	}
}

func TestSharedV2ReadbackFailureIncludingAmbiguousAcknowledgement(t *testing.T) {
	s, f := sharedFixture(t)
	if err := s.ThreadSettings().Set(65535, value(100)); err != nil {
		t.Fatal(err)
	}
	if err := s.MatterBlobs().Set("blob", []byte("old")); err != nil {
		t.Fatal(err)
	}
	clean := f.reboot()
	st, err := openSharedV2(clean, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MatterBlobs().Set("blob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= clean.steps; step++ {
		g := f.reboot()
		candidate, err := openSharedV2(g, owner)
		if err != nil {
			t.Fatal(err)
		}
		g.readAfter = step
		if err := candidate.MatterBlobs().Set("blob", []byte("new")); err == nil {
			t.Fatal("readback error acknowledged", step)
		}
		if _, err := candidate.ThreadSettings().Get(65535, 0); err == nil {
			t.Fatal("other view ignored latch")
		}
		recovered, err := openSharedV2(g.reboot(), owner)
		if err != nil {
			t.Fatal(step, err)
		}
		p, err := recovered.ThreadSettings().Get(65535, 0)
		if err != nil || !bytes.Equal(p, value(100)) {
			t.Fatal("counter lost after ambiguous readback", step, err)
		}
	}
}
