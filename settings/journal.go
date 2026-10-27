// Package settings implements a bounded, crash-safe two-bank Settings journal.
// The C6 backend is experimental until the documented physical campaign passes.
package settings

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"sync"

	"github.com/hilli/gothread/platform"
)

const (
	PartitionStart = 0x3f8000
	PartitionSize  = 32768
	BankSize       = PartitionSize / 2
	SectorSize     = 4096
	MaxKeys        = 16
	MaxValues      = 32
	MaxValueBytes  = 512
	MaxStateBytes  = 4096
	headerSize     = 64
	directorySlots = 128
	descriptorSize = 32
	dataStart      = headerSize + directorySlots*descriptorSize
	retireOffset   = BankSize - 4
	schema         = 1
	commit         = uint32(0)
	erasedWord     = uint32(math.MaxUint32)
	recordMagic    = uint32(0x314e5854)
)

var (
	ErrUnformatted       = errors.New("settings: partition is unformatted")
	ErrCorrupt           = errors.New("settings: corrupt journal")
	ErrSchema            = errors.New("settings: unsupported schema")
	ErrOwner             = errors.New("settings: namespace belongs to another device")
	ErrAmbiguous         = errors.New("settings: ambiguous flash result; reopen or stop")
	ErrCapacity          = errors.New("settings: bounded capacity exceeded")
	ErrExhausted         = errors.New("settings: generation exhausted")
	ErrClosed            = errors.New("settings: closed")
	ErrIndex             = errors.New("settings: invalid list index")
	ErrOfflineRequired   = errors.New("settings: administrative reset requires radio disabled")
	ErrNativeUnavailable = errors.New("settings: native C6 storage is unavailable on this target")
)

type medium interface {
	read([]byte, int) error
	program([]byte, int) error
	eraseBank(int) error
	transaction(func() error) error
	resetAllowed() error
	release()
}

type Stats struct {
	Epoch        uint64 `json:"epoch"`
	Generation   uint64 `json:"generation"`
	Keys         int    `json:"keys"`
	Values       int    `json:"values"`
	StateBytes   int    `json:"state_bytes"`
	ProgramCalls uint64 `json:"program_calls"`
	ProgramBytes uint64 `json:"program_bytes"`
	EraseSectors uint64 `json:"erase_sectors"`
	Compactions  uint64 `json:"compactions"`
	Failures     uint64 `json:"failures"`
}

type state map[platform.SettingsKey][][]byte
type bank struct {
	number             int
	epoch, seq, parent uint64
	values             state
	slot, tail         int
	dirty              bool
	retired            uint32
}

// Store serializes all access. An uncertain write latches an error, never a
// success-shaped old RAM state. Close and reopen to resolve complete commits.
type Store struct {
	mu       sync.Mutex
	dev      medium
	owner    [8]byte
	fault    error
	closed   bool
	stats    Stats
	observer func(Step)
}

// Step is non-secret diagnostic metadata emitted outside cache-off execution.
// An observer must not re-enter Store. It is for controlled lab cuts, not an
// acknowledgement or evidence that physical durability has passed.
type Step struct {
	Phase         string
	Offset, Bytes int
	Generation    uint64
}

func (s *Store) SetObserver(fn func(Step)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.observer = fn
	return nil
}
func (s *Store) step(phase string, off, n int) {
	if s.observer != nil {
		s.observer(Step{phase, PartitionStart + off, n, s.stats.Generation + 1})
	}
}

var _ platform.Settings = (*Store)(nil)

func open(dev medium, owner [8]byte) (*Store, error) {
	if owner == [8]byte{} {
		return nil, ErrOwner
	}
	s := &Store{dev: dev, owner: owner}
	b, _, err := s.load()
	if err != nil {
		return nil, err
	}
	s.observe(b)
	return s, nil
}

// formatEmpty initializes only a completely erased partition. It never erases
// a corrupt/foreign namespace. Destructive recovery is a separate lab operation.
func formatEmpty(dev medium, owner [8]byte) error {
	if owner == [8]byte{} {
		return ErrOwner
	}
	if err := dev.resetAllowed(); err != nil {
		return err
	}
	var raw [PartitionSize]byte
	if err := dev.read(raw[:], 0); err != nil {
		return err
	}
	if !allErased(raw[:]) {
		return ErrCorrupt
	}
	s := &Store{dev: dev, owner: owner}
	return dev.transaction(func() error {
		if err := dev.resetAllowed(); err != nil {
			return err
		}
		return s.writeBank(0, 1, 0, 0, state{})
	})
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.dev.release()
	}
	return nil
}

func (s *Store) Statistics() Stats { s.mu.Lock(); defer s.mu.Unlock(); return s.stats }
func (s *Store) observe(b bank) {
	data, _ := encodeState(b.values)
	s.stats.Epoch, s.stats.Generation, s.stats.Keys = b.epoch, b.seq, len(b.values)
	s.stats.Values = countValues(b.values)
	s.stats.StateBytes = len(data)
}

func (s *Store) current() (bank, int, error) {
	if s.closed {
		return bank{}, -1, ErrClosed
	}
	if s.fault != nil {
		return bank{}, -1, s.fault
	}
	b, retire, err := s.load()
	if err != nil {
		s.fault = err
		s.stats.Failures++
		return bank{}, -1, err
	}
	s.observe(b)
	return b, retire, nil
}

func (s *Store) Get(key platform.SettingsKey, index int) ([]byte, error) {
	if index < 0 {
		return nil, ErrIndex
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _, err := s.current()
	if err != nil {
		return nil, err
	}
	values := b.values[key]
	if index >= len(values) {
		return nil, platform.ErrSettingsNotFound
	}
	return append([]byte{}, values[index]...), nil
}

func (s *Store) Set(key platform.SettingsKey, value []byte) error {
	if len(value) > MaxValueBytes {
		return ErrCapacity
	}
	return s.change(func(v state) error { v[key] = [][]byte{append([]byte{}, value...)}; return nil })
}
func (s *Store) Add(key platform.SettingsKey, value []byte) error {
	if len(value) > MaxValueBytes {
		return ErrCapacity
	}
	return s.change(func(v state) error { v[key] = append(v[key], append([]byte{}, value...)); return nil })
}
func (s *Store) Delete(key platform.SettingsKey, index int) error {
	if index < -1 {
		return ErrIndex
	}
	return s.change(func(v state) error {
		values := v[key]
		if len(values) == 0 || index >= len(values) {
			return platform.ErrSettingsNotFound
		}
		if index == -1 {
			delete(v, key)
			return nil
		}
		values = append(values[:index], values[index+1:]...)
		if len(values) == 0 {
			delete(v, key)
		} else {
			v[key] = values
		}
		return nil
	})
}

// Wipe atomically clears the entire namespace, not just credentials or counters.
// On C6 it requires a disabled radio; discard the old Instance and provision a
// new disposable dataset. Reusing an old key or a flash backup is not safe.
func (s *Store) Wipe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, retire, err := s.current()
	if err != nil {
		return err
	}
	if err := s.dev.resetAllowed(); err != nil {
		return err
	}
	return s.mutate(b, retire, state{}, true)
}

func (s *Store) change(fn func(state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, retire, err := s.current()
	if err != nil {
		return err
	}
	next := clone(b.values)
	if err := fn(next); err != nil {
		return err
	}
	if _, err := encodeState(next); err != nil {
		return err
	}
	return s.mutate(b, retire, next, false)
}

func (s *Store) mutate(b bank, retire int, next state, wipe bool) error {
	if b.seq == math.MaxUint64 {
		return ErrExhausted
	}
	var changed platform.SettingsKey
	if !wipe {
		found := false
		keys := map[platform.SettingsKey]bool{}
		for k := range b.values {
			keys[k] = true
		}
		for k := range next {
			keys[k] = true
		}
		for k := range keys {
			if !equalValues(b.values[k], next[k]) {
				if found {
					return ErrCorrupt
				}
				changed, found = k, true
			}
		}
		if !found {
			return nil
		}
	}
	payload := encodeValues(next[changed])
	compact := wipe || b.dirty || b.slot == directorySlots || b.tail+align4(len(payload)) > retireOffset
	if compact && b.epoch == math.MaxUint64 {
		return ErrExhausted
	}
	started := false
	err := s.dev.transaction(func() error {
		if wipe {
			if err := s.dev.resetAllowed(); err != nil {
				return err
			}
		}
		started = true
		if retire >= 0 {
			if err := s.retire(retire); err != nil {
				return err
			}
		}
		if compact {
			target := 1 - b.number
			s.step("before_bank_erase", target*BankSize, BankSize)
			if err := s.dev.eraseBank(target); err != nil {
				return err
			}
			s.step("after_bank_erase", target*BankSize, BankSize)
			s.stats.EraseSectors += BankSize / SectorSize
			if err := s.writeBank(target, b.epoch+1, b.seq+1, b.epoch, next); err != nil {
				return err
			}
			if err := s.retire(b.number); err != nil {
				return err
			}
			s.stats.Compactions++
			return nil
		}
		return s.append(b, changed, next[changed], payload)
	})
	if err != nil {
		s.stats.Failures++
		if started {
			s.fault = errors.Join(ErrAmbiguous, err)
			return s.fault
		}
		return err // no flash operation began (e.g. a quiescence timeout)
	}
	current, _, err := s.load()
	if err != nil || current.seq != b.seq+1 {
		s.fault = errors.Join(ErrAmbiguous, err)
		s.stats.Failures++
		return s.fault
	}
	s.observe(current)
	return nil
}

func (s *Store) write(p []byte, off int) error {
	if len(p) == 0 {
		return nil
	}
	if off < 0 || off%4 != 0 || len(p)%4 != 0 || off > PartitionSize-len(p) {
		return ErrCorrupt
	}
	s.stats.ProgramCalls++
	s.stats.ProgramBytes += uint64(len(p))
	phase := "program"
	relative := off % BankSize
	if relative == 56 {
		phase = "bank_commit"
	} else if relative == retireOffset {
		phase = "bank_retire"
	} else if relative >= headerSize && relative < dataStart && (relative-headerSize)%descriptorSize == 28 {
		phase = "record_commit"
	}
	s.step("before_"+phase, off, len(p))
	if err := s.dev.program(p, off); err != nil {
		return err
	}
	check := make([]byte, len(p))
	if err := s.dev.read(check, off); err != nil {
		return err
	}
	if !bytes.Equal(check, p) {
		return ErrCorrupt
	}
	s.step("after_"+phase, off, len(p))
	return nil
}
func (s *Store) retire(number int) error {
	return s.write([]byte{0, 0, 0, 0}, number*BankSize+retireOffset)
}

func (s *Store) writeBank(number int, epoch, seq, parent uint64, v state) error {
	payload, err := encodeState(v)
	if err != nil {
		return err
	}
	h := bytes.Repeat([]byte{255}, headerSize)
	copy(h, []byte("C6STOR01"))
	put32(h, 8, schema)
	put64(h, 12, epoch)
	put64(h, 20, seq)
	put64(h, 28, parent)
	copy(h[36:44], s.owner[:])
	put32(h, 44, uint32(len(payload)))
	put32(h, 48, crc32.ChecksumIEEE(payload))
	put32(h, 52, crc32.ChecksumIEEE(h[:52]))
	base := number * BankSize
	if err := s.write(h[:56], base); err != nil {
		return err
	}
	if err := s.write(pad(payload), base+dataStart); err != nil {
		return err
	}
	return s.write([]byte{0, 0, 0, 0}, base+56)
}

func (s *Store) append(b bank, key platform.SettingsKey, values [][]byte, payload []byte) error {
	d := bytes.Repeat([]byte{255}, descriptorSize)
	put32(d, 0, recordMagic)
	put64(d, 4, b.seq+1)
	binary.LittleEndian.PutUint16(d[12:], uint16(key))
	binary.LittleEndian.PutUint16(d[14:], uint16(len(values)))
	put32(d, 16, uint32(b.tail))
	put32(d, 20, uint32(len(payload)))
	h := crc32.NewIEEE()
	h.Write(d[:24])
	h.Write(payload)
	put32(d, 24, h.Sum32())
	off := b.number*BankSize + headerSize + b.slot*descriptorSize
	if err := s.write(d[:28], off); err != nil {
		return err
	}
	if err := s.write(pad(payload), b.number*BankSize+b.tail); err != nil {
		return err
	}
	return s.write([]byte{0, 0, 0, 0}, off+28)
}

func (s *Store) load() (bank, int, error) {
	var raw [PartitionSize]byte
	if err := s.dev.read(raw[:], 0); err != nil {
		return bank{}, -1, err
	}
	if allErased(raw[:]) {
		return bank{}, -1, ErrUnformatted
	}
	var banks []bank
	for i := 0; i < 2; i++ {
		p := raw[i*BankSize : (i+1)*BankSize]
		retired := u32(p, retireOffset)
		// Retirement is at the END of the bank and erased last. A partially
		// erased retired bank cannot be mistaken for an older active one.
		if retired == commit {
			continue
		}
		if u32(p, 56) != commit {
			if u32(p, 56) != erasedWord {
				return bank{}, -1, ErrAmbiguous
			}
			continue
		}
		b, err := decodeBank(p, s.owner, i)
		if err != nil {
			return bank{}, -1, err
		}
		b.retired = retired
		banks = append(banks, b)
	}
	if len(banks) == 0 {
		return bank{}, -1, ErrCorrupt
	}
	if len(banks) == 1 {
		if banks[0].retired != erasedWord {
			return bank{}, -1, ErrAmbiguous
		}
		return banks[0], -1, nil
	}
	sort.Slice(banks, func(i, j int) bool { return banks[i].epoch < banks[j].epoch })
	old, new := banks[0], banks[1]
	if new.epoch != old.epoch+1 || new.parent != old.epoch || new.seq <= old.seq || new.retired != erasedWord {
		return bank{}, -1, ErrAmbiguous
	}
	return new, old.number, nil
}

func decodeBank(p []byte, owner [8]byte, number int) (bank, error) {
	b := bank{number: number, values: state{}}
	if !bytes.Equal(p[:8], []byte("C6STOR01")) || crc32.ChecksumIEEE(p[:52]) != u32(p, 52) {
		return b, ErrCorrupt
	}
	if !allErased(p[60:64]) {
		return b, ErrCorrupt
	}
	if u32(p, 8) != schema {
		return b, ErrSchema
	}
	if !bytes.Equal(p[36:44], owner[:]) {
		return b, ErrOwner
	}
	b.epoch, b.seq, b.parent = u64(p, 12), u64(p, 20), u64(p, 28)
	if b.epoch == 0 || b.parent >= b.epoch || (b.epoch == 1 && b.parent != 0) {
		return b, ErrCorrupt
	}
	length := int(u32(p, 44))
	if length < 4 || length > MaxStateBytes || dataStart+align4(length) > retireOffset {
		return b, ErrCorrupt
	}
	payload := p[dataStart : dataStart+length]
	if crc32.ChecksumIEEE(payload) != u32(p, 48) || !allErased(p[dataStart+length:dataStart+align4(length)]) {
		return b, ErrCorrupt
	}
	var err error
	b.values, err = decodeState(payload)
	if err != nil {
		return b, err
	}
	b.tail = dataStart + align4(length)
	for slot := 0; slot < directorySlots; slot++ {
		d := p[headerSize+slot*descriptorSize : headerSize+(slot+1)*descriptorSize]
		if u32(d, 28) != commit {
			if u32(d, 28) != erasedWord {
				return b, ErrAmbiguous
			}
			if !allErased(p[headerSize+(slot+1)*descriptorSize : dataStart]) {
				return b, ErrCorrupt
			}
			b.slot = slot
			b.dirty = !allErased(d)
			if !b.dirty && !allErased(p[b.tail:retireOffset]) {
				return b, ErrCorrupt
			}
			return b, nil
		}
		length, off := int(u32(d, 20)), int(u32(d, 16))
		if u32(d, 0) != recordMagic || u64(d, 4) != b.seq+1 || b.seq == math.MaxUint64 ||
			off != b.tail || length > MaxStateBytes || length < 0 || off > retireOffset-align4(length) {
			return b, ErrCorrupt
		}
		payload := p[off : off+length]
		h := crc32.NewIEEE()
		h.Write(d[:24])
		h.Write(payload)
		if h.Sum32() != u32(d, 24) || !allErased(p[off+length:off+align4(length)]) {
			return b, ErrCorrupt
		}
		count := int(binary.LittleEndian.Uint16(d[14:]))
		values, err := decodeValues(payload, count)
		if err != nil {
			return b, err
		}
		key := platform.SettingsKey(binary.LittleEndian.Uint16(d[12:]))
		if count == 0 {
			delete(b.values, key)
		} else {
			b.values[key] = values
		}
		if _, err := encodeState(b.values); err != nil {
			return b, ErrCorrupt
		}
		b.seq++
		b.tail += align4(length)
		b.slot = slot + 1
	}
	if !allErased(p[b.tail:retireOffset]) {
		return b, ErrCorrupt
	}
	return b, nil
}

func clone(v state) state {
	out := state{}
	for k, vs := range v {
		for _, p := range vs {
			out[k] = append(out[k], append([]byte{}, p...))
		}
	}
	return out
}
func countValues(v state) int {
	n := 0
	for _, p := range v {
		n += len(p)
	}
	return n
}
func equalValues(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
func encodeValues(v [][]byte) []byte {
	var p []byte
	for _, value := range v {
		p = binary.LittleEndian.AppendUint16(p, uint16(len(value)))
		p = append(p, value...)
	}
	return p
}
func decodeValues(p []byte, count int) ([][]byte, error) {
	if count > MaxValues {
		return nil, ErrCorrupt
	}
	values := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		if len(p) < 2 {
			return nil, ErrCorrupt
		}
		n := int(binary.LittleEndian.Uint16(p))
		p = p[2:]
		if n > MaxValueBytes || n > len(p) {
			return nil, ErrCorrupt
		}
		values = append(values, append([]byte{}, p[:n]...))
		p = p[n:]
	}
	if len(p) != 0 {
		return nil, ErrCorrupt
	}
	return values, nil
}
func encodeState(v state) ([]byte, error) {
	if len(v) > MaxKeys || countValues(v) > MaxValues {
		return nil, ErrCapacity
	}
	keys := make([]int, 0, len(v))
	for k := range v {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	p := binary.LittleEndian.AppendUint16(nil, uint16(len(keys)))
	p = binary.LittleEndian.AppendUint16(p, uint16(countValues(v)))
	for _, k := range keys {
		values := v[platform.SettingsKey(k)]
		if len(values) == 0 {
			return nil, ErrCapacity
		}
		for _, value := range values {
			if len(value) > MaxValueBytes {
				return nil, ErrCapacity
			}
		}
		p = binary.LittleEndian.AppendUint16(p, uint16(k))
		p = binary.LittleEndian.AppendUint16(p, uint16(len(values)))
		p = append(p, encodeValues(values)...)
	}
	if len(p) > MaxStateBytes {
		return nil, ErrCapacity
	}
	return p, nil
}
func decodeState(p []byte) (state, error) {
	v := state{}
	if len(p) < 4 {
		return nil, ErrCorrupt
	}
	keys, values := int(binary.LittleEndian.Uint16(p)), int(binary.LittleEndian.Uint16(p[2:]))
	p = p[4:]
	if keys > MaxKeys || values > MaxValues {
		return nil, ErrCorrupt
	}
	last := -1
	total := 0
	for i := 0; i < keys; i++ {
		if len(p) < 4 {
			return nil, ErrCorrupt
		}
		key, count := int(binary.LittleEndian.Uint16(p)), int(binary.LittleEndian.Uint16(p[2:]))
		p = p[4:]
		if key <= last || count == 0 || count > MaxValues {
			return nil, ErrCorrupt
		}
		last = key
		for j := 0; j < count; j++ {
			if len(p) < 2 {
				return nil, ErrCorrupt
			}
			n := int(binary.LittleEndian.Uint16(p))
			p = p[2:]
			if n > MaxValueBytes || n > len(p) {
				return nil, ErrCorrupt
			}
			v[platform.SettingsKey(key)] = append(v[platform.SettingsKey(key)], append([]byte{}, p[:n]...))
			p = p[n:]
			total++
		}
	}
	if len(p) != 0 || total != values {
		return nil, ErrCorrupt
	}
	return v, nil
}
func align4(n int) int    { return (n + 3) &^ 3 }
func pad(p []byte) []byte { q := bytes.Repeat([]byte{255}, align4(len(p))); copy(q, p); return q }
func allErased(p []byte) bool {
	for _, b := range p {
		if b != 255 {
			return false
		}
	}
	return true
}
func u32(p []byte, off int) uint32      { return binary.LittleEndian.Uint32(p[off:]) }
func u64(p []byte, off int) uint64      { return binary.LittleEndian.Uint64(p[off:]) }
func put32(p []byte, off int, n uint32) { binary.LittleEndian.PutUint32(p[off:], n) }
func put64(p []byte, off int, n uint64) { binary.LittleEndian.PutUint64(p[off:], n) }

func (s Stats) String() string {
	return fmt.Sprintf("epoch=%d generation=%d keys=%d values=%d bytes=%d", s.Epoch, s.Generation, s.Keys, s.Values, s.StateBytes)
}
