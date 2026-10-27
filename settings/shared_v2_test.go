package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"strings"
	"testing"
	"unsafe"

	"github.com/hilli/gothread/platform"
)

var sharedCut = errors.New("synthetic shared NOR interruption")

type sharedNOR struct {
	data                                   [SharedV2PartitionSize]byte
	steps, cut, programs, erases, releases int
	mode                                   string
	readError                              bool
	readAfter                              int
	offline                                bool
}

func newSharedNOR() *sharedNOR {
	f := &sharedNOR{offline: true}
	for i := range f.data {
		f.data[i] = 255
	}
	return f
}
func (f *sharedNOR) read(p []byte, off int) error {
	if f.readError || (f.readAfter > 0 && f.steps >= f.readAfter) {
		return sharedCut
	}
	if off < 0 || off > len(f.data)-len(p) {
		return ErrCorrupt
	}
	copy(p, f.data[off:off+len(p)])
	return nil
}
func (f *sharedNOR) program(p []byte, off int) error {
	if len(p) == 0 || len(p) > 256 || len(p)%4 != 0 || off < 0 || off%4 != 0 ||
		off > len(f.data)-len(p) || (off&255)+len(p) > 256 {
		return ErrCorrupt
	}
	f.programs++
	for i := 0; i < len(p); i += 4 {
		f.steps++
		cut := f.cut == f.steps
		if cut && f.mode == "before" {
			return sharedCut
		}
		for j := 0; j < 4; j++ {
			b := p[i+j]
			if f.data[off+i+j]&b != b {
				return ErrCorrupt
			}
			if cut && f.mode == "torn" {
				b |= 0xaa
			}
			f.data[off+i+j] &= b
		}
		if cut {
			return sharedCut
		}
	}
	return nil
}
func (f *sharedNOR) eraseSector(off int) error {
	if off < 0 || off%4096 != 0 || off > len(f.data)-4096 {
		return ErrCorrupt
	}
	f.steps++
	f.erases++
	cut := f.steps == f.cut
	if cut && f.mode == "before" {
		return sharedCut
	}
	for i := 0; i < 4096; i++ {
		if cut && f.mode == "torn" {
			f.data[off+i] |= 0x55
		} else {
			f.data[off+i] = 255
		}
	}
	if cut {
		return sharedCut
	}
	return nil
}
func (f *sharedNOR) transaction(fn func() error) error { return fn() }
func (f *sharedNOR) closeAllowed() error {
	if !f.offline {
		return ErrOfflineRequired
	}
	return nil
}
func (f *sharedNOR) release() { f.releases++ }
func (f *sharedNOR) reboot() *sharedNOR {
	g := *f
	g.steps, g.cut, g.programs, g.erases, g.releases = 0, 0, 0, 0, 0
	g.mode = ""
	g.readError = false
	g.readAfter = 0
	return &g
}
func sharedFixture(t *testing.T) (*SharedV2, *sharedNOR) {
	t.Helper()
	f := newSharedNOR()
	if err := formatSharedV2(f, owner); err != nil {
		t.Fatal(err)
	}
	s, err := openSharedV2(f, owner)
	if err != nil {
		t.Fatal(err)
	}
	f.steps, f.programs, f.erases = 0, 0, 0
	return s, f
}
func requireSharedBlob(t *testing.T, s *SharedV2, key string, want []byte) {
	t.Helper()
	got, err := s.MatterBlobs().Get(key)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s: %v (%d bytes)", key, err, len(got))
	}
}

func TestSharedV2ExactNamespacesKeysListsAndOwnership(t *testing.T) {
	s, f := sharedFixture(t)
	thread := s.ThreadSettings()
	matter := s.MatterBlobs()
	if err := thread.Set(0x6162, []byte("thread")); err != nil {
		t.Fatal(err)
	}
	input := []byte("matter")
	if err := matter.Set("ba", input); err != nil {
		t.Fatal(err)
	}
	input[0] = 'x'
	requireSharedBlob(t, s, "ba", []byte("matter"))
	if p, err := thread.Get(0x6162, 0); err != nil || string(p) != "thread" {
		t.Fatal(err, string(p))
	}
	for _, key := range []platform.SettingsKey{0, 255, 256, 32768, 65535} {
		if err := thread.Set(key, value(uint64(key))); err != nil {
			t.Fatal(err)
		}
		if p, err := thread.Get(key, 0); err != nil || !bytes.Equal(p, value(uint64(key))) {
			t.Fatal(key, err)
		}
	}
	if err := matter.Set("empty", nil); err != nil {
		t.Fatal(err)
	}
	if p, err := matter.Get("empty"); err != nil || len(p) != 0 {
		t.Fatal(err)
	}
	if _, err := matter.Get("absent"); err != ErrBlobNotFound {
		t.Fatal(err)
	}
	for _, key := range []string{"node/a", "node/é", "node/e\u0301", "other"} {
		if err := matter.Set(key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := matter.Keys("node/")
	if err != nil || strings.Join(keys, ",") != "node/a,node/e\u0301,node/é" {
		t.Fatal(keys, err)
	}
	partial, err := matter.Keys("node/\xc3")
	if err != nil || len(partial) != 1 || partial[0] != "node/é" {
		t.Fatal(partial, err)
	}
	for _, key := range []string{"", "nul\x00key", "\xff", strings.Repeat("x", 97)} {
		if err := matter.Set(key, nil); err != ErrSharedKey {
			t.Fatal(key, err)
		}
	}
	if err := thread.Set(9, nil); err != nil {
		t.Fatal(err)
	}
	if err := thread.Add(9, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := thread.Add(9, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if err := thread.Delete(9, 1); err != nil {
		t.Fatal(err)
	}
	if p, err := thread.Get(9, 1); err != nil || !bytes.Equal(p, []byte{2}) {
		t.Fatal(p, err)
	}
	if err := thread.Delete(9, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Get(9, 0); err != platform.ErrSettingsNotFound {
		t.Fatal(err)
	}
	if _, err := thread.Get(9, -1); err != ErrIndex {
		t.Fatal(err)
	}
	before := f.steps
	if err := thread.Wipe(); err != ErrSharedScope || f.steps != before {
		t.Fatal(err)
	}
	if err := matter.Delete("ba"); err != nil {
		t.Fatal(err)
	}
	if p, err := thread.Get(0x6162, 0); err != nil || string(p) != "thread" {
		t.Fatal(err)
	}
	if err := matter.Close(); err != nil || f.releases != 0 {
		t.Fatal("view released physical lease", err)
	}
	if _, err := matter.Get("empty"); err != ErrClosed {
		t.Fatal(err)
	}
	if _, err := thread.Get(0, 0); err != nil {
		t.Fatal(err)
	}
	f.offline = false
	if err := s.Close(); err != ErrOfflineRequired || f.releases != 0 {
		t.Fatal(err)
	}
	f.offline = true
	if err := s.Close(); err != nil || f.releases != 1 {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil || f.releases != 1 {
		t.Fatal(err)
	}
	if _, err := thread.Get(0, 0); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestSharedV2BlankCheckIncludesLegacyTailAndProfile(t *testing.T) {
	f := newSharedNOR()
	if _, err := openSharedV2(f, owner); err != ErrUnformatted {
		t.Fatal(err)
	}
	copy(f.data[PartitionStart-SharedV2PartitionStart:], "C6STOR01")
	if _, err := openSharedV2(f, owner); err == nil || err == ErrUnformatted {
		t.Fatal("legacy tail mistaken for blank", err)
	}
	if err := formatSharedV2(f, owner); err == nil || f.steps != 0 {
		t.Fatal("legacy data reformatted", err)
	}
	s, f := sharedFixture(t)
	if _, err := openSharedV2(f, [8]byte{1}); err != ErrOwner {
		t.Fatal(err)
	}
	f.data[12] = 2
	sharedPut32(f.data[:], 108, sharedCRC(0, f.data[:108]))
	if _, err := openSharedV2(f, owner); err != ErrSharedProfile {
		t.Fatal(err)
	}
	if f.erases != 0 {
		t.Fatal("Open erased foreign profile")
	}
	_ = s
}

func TestSharedV2CorruptionAndReadErrorsLatchBothConsumers(t *testing.T) {
	s, f := sharedFixture(t)
	if err := s.ThreadSettings().Set(65535, value(100)); err != nil {
		t.Fatal(err)
	}
	if err := s.MatterBlobs().Set("matter/key", []byte{7, 8, 9}); err != nil {
		t.Fatal(err)
	}
	at, ok := s.find(sharedMatter, []byte("matter/key"))
	if !ok {
		t.Fatal("key missing from actual committed index")
	}
	ref := s.values[s.entries[at].first]
	f.data[ref.offset] &= ^byte(1)
	if _, err := s.MatterBlobs().Get("matter/key"); err == nil {
		t.Fatal("committed CRC ignored")
	}
	fault := s.fault
	if _, err := s.ThreadSettings().Get(65535, 0); err != fault {
		t.Fatal("fault isolated by namespace", err)
	}
	if err := s.ThreadSettings().Set(1, []byte{1}); err != fault {
		t.Fatal(err)
	}
	s, f = sharedFixture(t)
	f.readError = true
	if _, err := s.MatterBlobs().Keys(""); err != sharedCut {
		t.Fatal(err)
	}
	f.readError = false
	if _, err := s.ThreadSettings().Get(1, 0); err != sharedCut {
		t.Fatal("latched read error hidden", err)
	}
}

func TestSharedV2EveryAppendAndCompactionBoundary(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint("compaction=", compact), func(t *testing.T) {
			s, f := sharedFixture(t)
			if err := s.ThreadSettings().Set(65535, value(100)); err != nil {
				t.Fatal(err)
			}
			if err := s.MatterBlobs().Set("value", bytes.Repeat([]byte{0x31}, 279)); err != nil {
				t.Fatal(err)
			}
			if compact {
				f.data[int(s.active)*SharedV2BankSize+int(s.banks[s.active].tail)+8] = 0
			}
			clean := f.reboot()
			st, err := openSharedV2(clean, owner)
			if err != nil {
				t.Fatal(err)
			}
			next := bytes.Repeat([]byte{0x72}, 517)
			if err := st.MatterBlobs().Set("value", next); err != nil {
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
					if err := candidate.MatterBlobs().Set("value", next); err == nil {
						t.Fatal("fault acknowledged", mode, cut)
					}
					if _, err := candidate.ThreadSettings().Get(65535, 0); err == nil || err == platform.ErrSettingsNotFound {
						t.Fatal("other consumer survived uncertainty", cut)
					}
					recovered, err := openSharedV2(g.reboot(), owner)
					if err != nil {
						if err != ErrAmbiguous && err != ErrCorrupt {
							t.Fatal(mode, cut, err)
						}
						continue
					}
					p, err := recovered.ThreadSettings().Get(65535, 0)
					if err != nil || !bytes.Equal(p, value(100)) {
						t.Fatal("Thread reservation lost during Matter recovery", mode, cut, err)
					}
					p, err = recovered.MatterBlobs().Get("value")
					old := bytes.Repeat([]byte{0x31}, 279)
					if err != nil || (!bytes.Equal(p, old) && !bytes.Equal(p, next)) {
						t.Fatal("partial blob", mode, cut, err)
					}
				}
			}
		})
	}
}

func TestSharedV2AcknowledgedRetirementPreventsFallback(t *testing.T) {
	s, f := sharedFixture(t)
	if err := s.ThreadSettings().Set(65535, value(100)); err != nil {
		t.Fatal(err)
	}
	f.data[int(s.active)*SharedV2BankSize+int(s.banks[s.active].tail)+8] = 0
	if err := s.MatterBlobs().Set("new", []byte("acknowledged")); err != nil {
		t.Fatal(err)
	}
	current := int(s.active) * SharedV2BankSize
	for i := 0; i < 4; i++ {
		f.data[current+112+i] = 255
	}
	if _, err := openSharedV2(f.reboot(), owner); err == nil {
		t.Fatal("acknowledged new bank fell back to old unretired state")
	}
}

func fillSharedHardMax(t *testing.T, s *SharedV2) {
	t.Helper()
	matter := s.MatterBlobs()
	left := 65536 - 32*(8+96+4)
	for i := 0; i < 32; i++ {
		n := min(left, 16384)
		left -= n
		if err := matter.Set(fmt.Sprintf("m%095d", i), bytes.Repeat([]byte{byte(i + 1)}, n)); err != nil {
			t.Fatal(i, err)
		}
	}
	thread := s.ThreadSettings()
	for i := 0; i < 16; i++ {
		count := 2
		if i == 0 {
			count = 1
		}
		if i == 1 {
			count = 3
		}
		for j := 0; j < count; j++ {
			n := 121
			if i == 0 {
				n = 52
			}
			if i == 1 && j == 0 {
				n = 126
			}
			var err error
			if j == 0 {
				err = thread.Set(platform.SettingsKey(0xf000+i), make([]byte, n))
			} else {
				err = thread.Add(platform.SettingsKey(0xf000+i), make([]byte, n))
			}
			if err != nil {
				t.Fatal(i, j, err)
			}
		}
	}
}
func TestSharedV2HardMaxColdAnd64SmallWritesWithoutErase(t *testing.T) {
	s, f := sharedFixture(t)
	fillSharedHardMax(t, s)
	if s.stats.MatterEntryBytes != 65536 || s.stats.ThreadEntryBytes != 4096 {
		t.Fatal(s.stats)
	}
	before := f.steps
	if err := s.MatterBlobs().Set("extra", nil); err != ErrCapacity || f.steps != before {
		t.Fatal("quota borrowed or flash changed", err)
	}
	if err := s.ThreadSettings().Set(0xffff, nil); err != ErrCapacity || f.steps != before {
		t.Fatal("Thread quota borrowed", err)
	}
	if err := s.MatterBlobs().Set(fmt.Sprintf("m%095d", 3), make([]byte, 12929)); err != ErrCapacity || f.steps != before {
		t.Fatal("Matter encoded-byte quota borrowed from Thread", err)
	}
	if err := s.ThreadSettings().Set(0xf000, make([]byte, 53)); err != ErrCapacity || f.steps != before {
		t.Fatal("Thread encoded-byte quota borrowed from Matter", err)
	}
	if err := s.MatterBlobs().Set(fmt.Sprintf("m%095d", 0), make([]byte, 16385)); err != ErrCapacity || f.steps != before {
		t.Fatal(err)
	}
	f.data[int(s.active)*SharedV2BankSize+int(s.banks[s.active].tail)+8] = 0
	startErases := f.erases
	if err := s.MatterBlobs().Set(fmt.Sprintf("m%095d", 0), make([]byte, 16384)); err != nil {
		t.Fatal(err)
	}
	if f.erases-startErases != 24 || s.banks[s.active].snapshotBytes != 69640 || s.payloadBytes != 16488 {
		t.Fatal("hard profile budgets not exercised", s.stats, s.banks[s.active])
	}
	t.Logf("cold model totals: %+v; bank=%+v", s.stats, s.banks[s.active])
	afterCold := f.erases
	tail := s.banks[s.active].tail
	for i := 0; i < 64; i++ {
		p := make([]byte, 52)
		p[0] = byte(i)
		if err := s.ThreadSettings().Set(0xf000, p); err != nil {
			t.Fatal(i, err)
		}
	}
	if f.erases != afterCold || s.banks[s.active].slot != 65 || s.banks[s.active].tail-tail != 4096 {
		t.Fatal("reserved bytes/slots lost", s.stats, s.banks[s.active])
	}
	t.Logf("64-small model totals: %+v; bank=%+v", s.stats, s.banks[s.active])
	if err := s.MatterBlobs().Set(fmt.Sprintf("m%095d", 0), make([]byte, 16384)); err != nil {
		t.Fatal(err)
	}
	if f.erases != afterCold+24 {
		t.Fatal("next cold append failed to preserve fresh hot reserve")
	}
}

func TestSharedV2CRCAndFixedObjectAccounting(t *testing.T) {
	p := []byte("123456789")
	if sharedCRC(0, p) != crc32.ChecksumIEEE(p) || sharedCRC(sharedCRC(0, p[:3]), p[3:]) != crc32.ChecksumIEEE(p) {
		t.Fatal("CRC is not IEEE streaming")
	}
	if unsafe.Sizeof(sharedEntry{}) != 32 || unsafe.Sizeof(sharedValue{}) != 8 {
		t.Fatal("fixed index layout drift")
	}
	t.Logf("host owner object=%d; target object/closure/native/stack proof still required", unsafe.Sizeof(SharedV2{}))
	if unsafe.Sizeof(SharedV2{}) > 9000 {
		t.Fatal("fixed owner unexpectedly grew")
	}
	if sharedArenaBytes != 94076 || sharedMaxSnapshot+16488+4096 != 90224 || sharedArenaBytes-90224 != 3852 {
		t.Fatal("hard profile arithmetic")
	}
}

func TestSharedV2InventoryRecomputedFrom225SanitizedInputs(t *testing.T) {
	var fixture struct {
		Source    string `json:"serializer_source"`
		Snapshots []struct {
			Records [][2]int `json:"records"`
		} `json:"snapshots"`
	}
	p, err := os.ReadFile("testdata/shared-v2-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Source != "1659f710eee769f32b2ed477cba683e490ed1beb" || len(fixture.Snapshots) != 225 {
		t.Fatal("wrong inventory source")
	}
	maxMatter, maxSnapshot, maxTransaction, maxRaw, maxKeys, maxValue, maxKey := 0, 0, 0, 0, 0, 0, 0
	threadBytes := 0
	for _, n := range []int{254, 254, 49} {
		threadBytes += 8 + 2 + 4 + n
	}
	for _, snapshot := range fixture.Snapshots {
		live, raw := 0, 4+3*6+254+254+49
		if len(snapshot.Records) > 32 {
			t.Fatal("selected key quota")
		}
		for _, record := range snapshot.Records {
			key, n := record[0], record[1]
			if key < 1 || key > 96 || n < 0 || n > 16384 {
				t.Fatal("selected record exceeds profile", record)
			}
			live += 8 + key + 4 + n
			raw += 6 + n
			maxTransaction = max(maxTransaction, int(sharedAlign(uint32(key+4+4+n))))
			maxValue = max(maxValue, n)
			maxKey = max(maxKey, key)
		}
		if live > 65536 || threadBytes > 4096 {
			t.Fatal("nonborrowable selected quota")
		}
		maxMatter = max(maxMatter, live)
		maxSnapshot = max(maxSnapshot, 8+live+threadBytes)
		maxRaw = max(maxRaw, raw)
		maxKeys = max(maxKeys, len(snapshot.Records)+3)
	}
	if maxMatter != 60042 || maxSnapshot != 60649 || maxTransaction != 9220 || maxRaw != 60027 || maxKeys != 25 || maxValue != 9186 || maxKey != 39 {
		t.Fatal(maxMatter, maxSnapshot, maxTransaction, maxRaw, maxKeys, maxValue, maxKey)
	}
}

func TestSharedV2GenerationAndEpochExhaustionPreflight(t *testing.T) {
	for _, epoch := range []bool{false, true} {
		_, f := sharedFixture(t)
		sharedPut64(f.data[:], 32, math.MaxUint64)
		sharedPut64(f.data[:], 48, math.MaxUint64-1)
		if !epoch {
			sharedPut64(f.data[:], 40, math.MaxUint64)
		}
		sharedPut32(f.data[:], 108, sharedCRC(0, f.data[:108]))
		s, err := openSharedV2(f, owner)
		if err != nil {
			t.Fatal(err)
		}
		if epoch {
			f.data[int(s.banks[s.active].tail)+8] = 0
		}
		before := f.steps
		if err := s.MatterBlobs().Set("no", nil); err != ErrExhausted || f.steps != before {
			t.Fatal(epoch, err)
		}
	}
}
