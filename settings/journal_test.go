package settings

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"testing"

	"github.com/hilli/gothread/platform"
)

var owner = [8]byte{0x60, 1, 2, 0xff, 0xfe, 3, 4, 5}
var errCut = errors.New("synthetic controlled power interruption")

// flashModel enforces the C6 geometry/1->0 constraint. Every word program and
// sector erase is an independent failure boundary; no physical result is implied.
type flashModel struct {
	data       [PartitionSize]byte
	steps, cut int
	mode       string
	readError  bool
	readAfter  int
	erases     int
	offline    bool
}

func blank() *flashModel {
	f := &flashModel{offline: true}
	for i := range f.data {
		f.data[i] = 255
	}
	return f
}
func (f *flashModel) read(p []byte, off int) error {
	if f.readError || (f.readAfter > 0 && f.steps >= f.readAfter) {
		return errors.New("synthetic read failure")
	}
	if off < 0 || off > PartitionSize-len(p) {
		return ErrCorrupt
	}
	copy(p, f.data[off:off+len(p)])
	return nil
}
func (f *flashModel) program(p []byte, off int) error {
	if off < 0 || off%4 != 0 || len(p)%4 != 0 || off > PartitionSize-len(p) {
		return ErrCorrupt
	}
	for i := 0; i < len(p); i += 4 {
		for j := 0; j < 4; j++ {
			if f.data[off+i+j]&p[i+j] != p[i+j] {
				return errors.New("program attempted 0->1")
			}
		}
		f.steps++
		cut := f.cut == f.steps
		if cut && f.mode == "before" {
			return errCut
		}
		for j := 0; j < 4; j++ {
			b := p[i+j]
			if cut && f.mode == "torn" {
				b |= 0xaa
			}
			f.data[off+i+j] &= b
		}
		if cut {
			return errCut
		}
	}
	return nil
}
func (f *flashModel) eraseBank(bank int) error {
	if bank < 0 || bank > 1 {
		return ErrCorrupt
	}
	for sector := 0; sector < BankSize/SectorSize; sector++ {
		f.steps++
		f.erases++
		cut := f.cut == f.steps
		if cut && f.mode == "before" {
			return errCut
		}
		start := bank*BankSize + sector*SectorSize
		end := start + SectorSize
		if cut && f.mode == "torn" {
			end = start + SectorSize/2
		}
		for i := start; i < end; i++ {
			f.data[i] = 255
		}
		if cut {
			return errCut
		}
	}
	return nil
}
func (f *flashModel) transaction(fn func() error) error { return fn() }
func (f *flashModel) resetAllowed() error {
	if !f.offline {
		return ErrOfflineRequired
	}
	return nil
}
func (f *flashModel) release() {}
func (f *flashModel) reboot() *flashModel {
	g := *f
	g.cut = 0
	g.steps = 0
	g.mode = ""
	g.readError = false
	g.readAfter = 0
	return &g
}
func formatted(t *testing.T) (*Store, *flashModel) {
	t.Helper()
	f := blank()
	if err := formatEmpty(f, owner); err != nil {
		t.Fatal(err)
	}
	s, err := open(f, owner)
	if err != nil {
		t.Fatal(err)
	}
	f.steps = 0
	return s, f
}
func value(n uint64) []byte { return binary.LittleEndian.AppendUint64(nil, n) }

func TestSettingsListSemanticsAndCapacity(t *testing.T) {
	s, f := formatted(t)
	if _, err := s.Get(1, 0); !errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Get(1, -1); err != ErrIndex {
		t.Fatal(err)
	}
	if err := s.Set(1, []byte{}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(1, 0); err != nil || len(v) != 0 {
		t.Fatal(v, err)
	}
	for _, p := range [][]byte{{1}, {2}, {3}} {
		if err := s.Add(2, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(2, 1); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(2, 1); err != nil || !bytes.Equal(v, []byte{3}) {
		t.Fatal(v, err)
	}
	if err := s.Set(2, []byte{7}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(2, 1); !errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	if err := s.Delete(2, -1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(2, 0); !errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	if err := s.Delete(1, -2); err != ErrIndex {
		t.Fatal(err)
	}
	if err := s.Set(3, make([]byte, MaxValueBytes+1)); err != ErrCapacity {
		t.Fatal(err)
	}
	for i := 0; i < MaxKeys-1; i++ {
		if err := s.Set(platform.SettingsKey(10+i), []byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	before := f.data
	if err := s.Set(100, []byte{1}); err != ErrCapacity || f.data != before {
		t.Fatal("capacity mutation", err)
	}
	if err := s.Wipe(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(1, 0); !errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	if err := s.Set(1, []byte{8}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(1, 0)
	got[0] = 99
	got, err := s.Get(1, 0)
	if err != nil || got[0] != 8 {
		t.Fatal("borrowed mutable settings", got, err)
	}
}

func TestFormatOwnerCorruptionAndReadFailures(t *testing.T) {
	f := blank()
	if _, err := open(f, owner); err != ErrUnformatted {
		t.Fatal(err)
	}
	if f.erases != 0 {
		t.Fatal("open erased storage")
	}
	f.readError = true
	if _, err := open(f, owner); err == nil || errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	f.readError = false
	if err := formatEmpty(f, owner); err != nil {
		t.Fatal(err)
	}
	if err := formatEmpty(f, owner); err != ErrCorrupt {
		t.Fatal("reformatted existing state", err)
	}
	wrong := owner
	wrong[7]++
	if _, err := open(f, wrong); err != ErrOwner {
		t.Fatal(err)
	}
	f.data[8] = 2
	put32(f.data[:], 52, crc32.ChecksumIEEE(f.data[:52]))
	if _, err := open(f, owner); err != ErrSchema {
		t.Fatal(err)
	}
	if f.erases != 0 {
		t.Fatal("corrupt schema caused erase")
	}
}

func assertRecovery(t *testing.T, f *flashModel, old, new []byte, ack uint64) {
	t.Helper()
	reopened, err := open(f.reboot(), owner)
	if err != nil {
		if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrAmbiguous) {
			t.Fatal("unexpected recovery error", err)
		}
		return // explicit fail-closed is never an older success
	}
	got, err := reopened.Get(9, 0)
	if err != nil {
		t.Fatal("lost acknowledged value", err)
	}
	if !bytes.Equal(got, old) && !bytes.Equal(got, new) {
		t.Fatalf("partial or rolled-back value %x", got)
	}
	if reopened.Statistics().Generation < ack {
		t.Fatal("rolled back acknowledged generation")
	}
}

func campaign(t *testing.T, base *flashModel, op func(*Store) error, old, new []byte) {
	t.Helper()
	clean := base.reboot()
	s, err := open(clean, owner)
	if err != nil {
		t.Fatal(err)
	}
	ack := s.Statistics().Generation
	if err := op(s); err != nil {
		t.Fatal("campaign did not reach a successful baseline", err)
	}
	steps := clean.steps
	if steps == 0 {
		t.Fatal("fault campaign exercised no flash operation")
	}
	for _, mode := range []string{"before", "after", "torn"} {
		for cut := 1; cut <= steps; cut++ {
			t.Run(fmt.Sprintf("%s/%d", mode, cut), func(t *testing.T) {
				f := base.reboot()
				s, err := open(f, owner)
				if err != nil {
					t.Fatal(err)
				}
				f.cut, f.mode = cut, mode
				if err := op(s); err == nil {
					t.Fatal("injected failure reported success")
				}
				if !errors.Is(s.fault, ErrAmbiguous) {
					t.Fatal("uncertain write was not latched", s.fault)
				}
				if _, err := s.Get(9, 0); !errors.Is(err, ErrAmbiguous) {
					t.Fatal("live handle exposed stale success", err)
				}
				assertRecovery(t, f, old, new, ack)
			})
		}
	}
}

func TestEveryAppendWordFailureAndAmbiguousCommit(t *testing.T) {
	s, f := formatted(t)
	if err := s.Set(9, value(100)); err != nil {
		t.Fatal(err)
	}
	campaign(t, f, func(s *Store) error { return s.Set(9, value(200)) }, value(100), value(200))
}

func TestEveryCompactionStepAndRetirementFailure(t *testing.T) {
	s, f := formatted(t)
	old := bytes.Repeat([]byte{0x55}, 512)
	if err := s.Set(9, old); err != nil {
		t.Fatal(err)
	}
	// Force compaction by consuming all directory slots with small records.
	for i := 0; i < directorySlots-1; i++ {
		if err := s.Set(8, value(uint64(i))); err != nil {
			t.Fatal(err)
		}
	}
	b, _, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if b.slot != directorySlots {
		t.Fatal("did not force compaction", b.slot)
	}
	next := bytes.Repeat([]byte{0x11}, 512)
	campaign(t, f, func(s *Store) error { return s.Set(9, next) }, old, next)
}

func TestAcknowledgedCompactionCannotFallBackToRetiredBank(t *testing.T) {
	s, f := formatted(t)
	for i := uint64(1); i <= 300; i++ {
		if err := s.Set(9, value(i)); err != nil {
			t.Fatal(err)
		}
		reopened, err := open(f.reboot(), owner)
		if err != nil {
			t.Fatal(err)
		}
		v, err := reopened.Get(9, 0)
		if err != nil || !bytes.Equal(v, value(i)) {
			t.Fatal(i, v, err)
		}
		if s.Statistics().Compactions > 0 {
			break
		} // inspect immediately after the acknowledgement of the first bank switch
	}
	b, _, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if b.epoch < 2 {
		t.Fatal("no compaction")
	}
	erasedCommit := f.reboot()
	for i := 0; i < 4; i++ {
		erasedCommit.data[b.number*BankSize+56+i] = 255
	}
	if _, err := open(erasedCommit, owner); err == nil {
		t.Fatal("erased current commit silently exposed a retired acknowledged namespace")
	}
	f.data[b.number*BankSize+48] ^= 1
	if _, err := open(f, owner); err == nil {
		t.Fatal("corrupted current bank silently rolled back")
	}
}

func TestBoundedValuesAndAggregateState(t *testing.T) {
	s, f := formatted(t)
	for i := 0; i < MaxValues; i++ {
		if err := s.Add(1, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	before := f.data
	if err := s.Add(1, []byte{99}); err != ErrCapacity || f.data != before {
		t.Fatal("value count was not bounded", err)
	}
	s, f = formatted(t)
	for i := 0; i < 7; i++ {
		if err := s.Set(platform.SettingsKey(i+1), make([]byte, 512)); err != nil {
			t.Fatal(err)
		}
	}
	before = f.data
	if err := s.Set(8, make([]byte, 512)); err != ErrCapacity || f.data != before {
		t.Fatal("aggregate state was not bounded", err)
	}
}

func TestCommittedRecordCorruptionIsNotAnIncompleteTail(t *testing.T) {
	s, f := formatted(t)
	if err := s.Set(9, value(1)); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{headerSize, headerSize + 4, headerSize + 16, headerSize + 24, headerSize + 28, dataStart + 4} {
		g := f.reboot()
		g.data[offset] ^= 1
		if _, err := open(g, owner); err == nil {
			t.Fatal("accepted corrupted committed data", offset)
		}
	}
}

func TestWipeIsWholeNamespaceOfflineAndAtomic(t *testing.T) {
	s, f := formatted(t)
	if err := s.Set(1, []byte("disposable dataset")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(9, value(100)); err != nil {
		t.Fatal(err)
	}
	f.offline = false
	before := f.data
	if err := s.Wipe(); err != ErrOfflineRequired || f.data != before {
		t.Fatal(err)
	}
	f.offline = true
	clean := f.reboot()
	st, err := open(clean, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Wipe(); err != nil {
		t.Fatal(err)
	}
	steps := clean.steps
	for cut := 1; cut <= steps; cut++ {
		g := f.reboot()
		st, err := open(g, owner)
		if err != nil {
			t.Fatal(err)
		}
		g.cut, g.mode = cut, "after"
		if err := st.Wipe(); err == nil {
			t.Fatal("wipe failure succeeded")
		}
		r, err := open(g.reboot(), owner)
		if err != nil {
			if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrAmbiguous) {
				t.Fatal(err)
			}
			continue
		}
		_, a := r.Get(1, 0)
		_, b := r.Get(9, 0)
		if (a == nil) != (b == nil) {
			t.Fatal("credential/counter-only wipe")
		}
	}
}

func TestBoundsExhaustionAndLatchedReadFailure(t *testing.T) {
	s, f := formatted(t)
	if err := s.Set(9, value(1)); err != nil {
		t.Fatal(err)
	}

	f.readError = true
	if _, err := s.Get(9, 0); err == nil || errors.Is(err, platform.ErrSettingsNotFound) {
		t.Fatal(err)
	}
	f.readError = false
	if err := s.Set(9, value(2)); err == nil {
		t.Fatal("read failure was silently cleared")
	}
	s, f = formatted(t)
	put64(f.data[:], 20, math.MaxUint64)
	put32(f.data[:], 52, crc32.ChecksumIEEE(f.data[:52]))
	s, err := open(f, owner)
	if err != nil {
		t.Fatal(err)
	}
	before := f.data
	if err := s.Set(9, value(2)); err != ErrExhausted || f.data != before {
		t.Fatal(err)
	}
}

func TestEveryFormatWordAndTornCommitFailsClosed(t *testing.T) {
	base := blank()
	clean := base.reboot()
	if err := formatEmpty(clean, owner); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"before", "after", "torn"} {
		for cut := 1; cut <= clean.steps; cut++ {
			f := base.reboot()
			f.cut, f.mode = cut, mode
			if err := formatEmpty(f, owner); err == nil {
				t.Fatal("format failure reported success")
			}
			_, err := open(f.reboot(), owner)
			if err != nil && !errors.Is(err, ErrUnformatted) && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrAmbiguous) {
				t.Fatal(err)
			}
			if f.erases != 0 {
				t.Fatal("format auto-erased invalid state")
			}
		}
	}
}

func TestListDeletionFaultsPreserveWholeSetting(t *testing.T) {
	s, f := formatted(t)
	for _, v := range [][]byte{{1}, {2}, {3}} {
		if err := s.Add(7, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []func(*Store) error{
		func(s *Store) error { return s.Delete(7, 1) },
		func(s *Store) error { return s.Delete(7, -1) },
		func(s *Store) error { return s.Add(7, []byte{4}) },
	} {
		clean := f.reboot()
		candidate, err := open(clean, owner)
		if err != nil {
			t.Fatal(err)
		}
		if err := operation(candidate); err != nil {
			t.Fatal(err)
		}
		expected, _, err := candidate.load()
		if err != nil {
			t.Fatal(err)
		}
		old, _, err := s.load()
		if err != nil {
			t.Fatal(err)
		}
		for cut := 1; cut <= clean.steps; cut++ {
			g := f.reboot()
			st, err := open(g, owner)
			if err != nil {
				t.Fatal(err)
			}
			g.cut, g.mode = cut, "torn"
			if err := operation(st); err == nil {
				t.Fatal("fault ignored")
			}
			st, err = open(g.reboot(), owner)
			if err != nil {
				if !errors.Is(err, ErrAmbiguous) && !errors.Is(err, ErrCorrupt) {
					t.Fatal(err)
				}
				continue
			}
			current, _, err := st.load()
			if err != nil {
				t.Fatal(err)
			}
			a, _ := encodeState(current.values)
			b, _ := encodeState(old.values)
			c, _ := encodeState(expected.values)
			if !bytes.Equal(a, b) && !bytes.Equal(a, c) {
				t.Fatal("partial list mutation")
			}
		}
	}
}

func TestReadbackFailureAtEachAppendBoundaryFailsClosed(t *testing.T) {
	s, f := formatted(t)
	if err := s.Set(1, value(10)); err != nil {
		t.Fatal(err)
	}
	baseline := f.reboot()
	candidate, err := open(baseline, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Set(1, value(11)); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= baseline.steps; step++ {
		g := f.reboot()
		st, err := open(g, owner)
		if err != nil {
			t.Fatal(err)
		}
		g.readAfter = step
		if err := st.Set(1, value(11)); err == nil {
			t.Fatal("readback failure acknowledged", step)
		}
		st, err = open(g.reboot(), owner)
		if err != nil {
			t.Fatal(err)
		}
		p, err := st.Get(1, 0)
		if err != nil {
			t.Fatal(err)
		}
		n := binary.LittleEndian.Uint64(p)
		if n != 10 && n != 11 {
			t.Fatal("acknowledged counter rollback or partial value", step, n)
		}
	}
}
