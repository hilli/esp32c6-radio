package settings

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"unicode/utf8"
)

const (
	SharedV2Profile        = "c6-shared-v2-lab1"
	SharedV2PartitionStart = 0x3d0000
	SharedV2PartitionSize  = 196608
	SharedV2BankSize       = 98304
	sharedHeaderSize       = 128
	sharedSlots            = 128
	sharedDescriptorSize   = 32
	sharedArenaStart       = sharedHeaderSize + sharedSlots*sharedDescriptorSize
	sharedRetire           = SharedV2BankSize - 4
	sharedArenaBytes       = sharedRetire - sharedArenaStart
	sharedMatterBytes      = 65536
	sharedThreadBytes      = 4096
	sharedMaxSnapshot      = 8 + sharedMatterBytes + sharedThreadBytes
	sharedMatterKeys       = 32
	sharedThreadKeys       = 16
	sharedMaxEntries       = sharedMatterKeys + sharedThreadKeys
	sharedMaxValues        = 64
	sharedMaxKey           = 96
	sharedMatterValue      = 16384
	sharedThreadValue      = 512
	sharedHotBytes         = 4096
	sharedHotSlots         = 64
	sharedSmallPayload     = 64
	sharedRecordMagic      = 0x32525854
	sharedThread           = 1
	sharedMatter           = 2
	sharedReplace          = 1
	sharedDelete           = 2
	sharedCallerOffset     = uint32(math.MaxUint32)
)

var (
	ErrSharedProfile   = errors.New("settings: wrong or unsupported shared-store profile")
	ErrSharedNamespace = errors.New("settings: invalid shared namespace")
	ErrSharedKey       = errors.New("settings: invalid exact shared-store key")
	ErrSharedScope     = errors.New("settings: shared namespace wipe is not authorized")
	ErrBlobNotFound    = errors.New("settings: blob not found")
)

// SharedV2Limits is a value, not mutable owner configuration.
type SharedV2Limits struct {
	Profile                                                                      string
	PartitionStart, PartitionBytes, BankBytes                                    int
	MatterKeys, MatterValues, MatterEntryBytes, MatterKeyBytes, MatterValueBytes int
	ThreadKeys, ThreadValues, ThreadEntryBytes, ThreadValueBytes                 int
	SnapshotBytes, ArenaBytes, SmallPayloadBytes, SmallWritesReserved            int
}

func sharedLimits() SharedV2Limits {
	return SharedV2Limits{
		SharedV2Profile, SharedV2PartitionStart, SharedV2PartitionSize, SharedV2BankSize,
		32, 32, sharedMatterBytes, sharedMaxKey, sharedMatterValue,
		16, 32, sharedThreadBytes, sharedThreadValue,
		sharedMaxSnapshot, sharedArenaBytes, sharedSmallPayload, sharedHotSlots,
	}
}

type sharedMedium interface {
	read([]byte, int) error
	program([]byte, int) error
	eraseSector(int) error
	transaction(func() error) error
	closeAllowed() error
	release()
}

type sharedValue struct{ offset, length uint32 }

// The fixed index stores flash references, never retained value copies.
type sharedEntry struct {
	source     uint32
	bytes      uint32
	generation uint64
	first      uint16
	count      uint16
	keyLen     uint16
	namespace  uint8
	flags      uint8
	reserved   [8]byte
}

type sharedBank struct {
	epoch, generation, base, parent  uint64
	snapshotBytes, snapshotCRC, tail uint32
	slot                             uint16
	valid, retired, dirty            bool
}

type SharedV2Stats struct {
	Epoch, Generation                                                            uint64
	ProgramCalls, ProgramBytes, EraseSectors, CopiedBytes, Compactions, Failures uint64
	MatterEntryBytes, ThreadEntryBytes                                           uint32
	MatterKeys, ThreadKeys, ThreadValues                                         uint16
}

// SharedV2 owns one physical lease. Views cannot release it. All uncertain
// writes and backing corruption latch this owner, including both namespaces.
type SharedV2 struct {
	keys         [sharedMaxEntries][sharedMaxKey]byte
	entries      [sharedMaxEntries]sharedEntry
	values       [sharedMaxValues]sharedValue
	stagedValues [32]sharedValue
	stream       [256]byte
	page         [64]uint32
	header       [sharedHeaderSize]byte
	descriptor   [sharedDescriptorSize]byte
	stagedKey    [sharedMaxKey]byte
	staged       sharedEntry

	mu                     sync.Mutex
	dev                    sharedMedium
	eui                    [8]byte
	fault                  error
	banks                  [2]sharedBank
	stats                  SharedV2Stats
	borrowed               []byte
	work                   func() error
	thread                 SharedThreadSettings
	matter                 SharedMatterBlobs
	keyCount, valueCount   uint16
	active, retirePending  int8
	pendingOp              uint8
	closed, began, compact bool
	payloadBytes           uint32
}

func (s *SharedV2) Limits() SharedV2Limits { return sharedLimits() }
func (s *SharedV2) Statistics() SharedV2Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func openSharedV2(dev sharedMedium, eui [8]byte) (*SharedV2, error) {
	if eui == [8]byte{} {
		return nil, ErrOwner
	}
	s := &SharedV2{dev: dev, eui: eui, retirePending: -1}
	s.thread.owner = s
	s.matter.owner = s
	s.work = s.commitWork
	if err := s.loadShared(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SharedV2) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := s.dev.closeAllowed(); err != nil {
		return err
	}
	s.closed = true
	s.borrowed = nil
	s.dev.release()
	return nil
}

func sharedCRC(crc uint32, p []byte) uint32 {
	crc = ^crc
	for _, b := range p {
		crc ^= uint32(b)
		for bit := 0; bit < 8; bit++ {
			mask := uint32(0) - (crc & 1)
			crc = (crc >> 1) ^ (0xedb88320 & mask)
		}
	}
	return ^crc
}
func sharedU16(p []byte, off int) uint16      { return binary.LittleEndian.Uint16(p[off:]) }
func sharedPut16(p []byte, off int, v uint16) { binary.LittleEndian.PutUint16(p[off:], v) }
func sharedU32(p []byte, off int) uint32      { return binary.LittleEndian.Uint32(p[off:]) }
func sharedPut32(p []byte, off int, v uint32) { binary.LittleEndian.PutUint32(p[off:], v) }
func sharedU64(p []byte, off int) uint64      { return binary.LittleEndian.Uint64(p[off:]) }
func sharedPut64(p []byte, off int, v uint64) { binary.LittleEndian.PutUint64(p[off:], v) }
func sharedAlign(v uint32) uint32             { return (v + 3) &^ 3 }
func sharedErased(p []byte) bool {
	for _, b := range p {
		if b != 255 {
			return false
		}
	}
	return true
}
func sharedCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
func sharedValidKey(ns uint8, key []byte) error {
	switch ns {
	case sharedThread:
		if len(key) != 2 {
			return ErrSharedKey
		}
	case sharedMatter:
		if len(key) == 0 || len(key) > sharedMaxKey || !utf8.Valid(key) {
			return ErrSharedKey
		}
		for _, b := range key {
			if b == 0 {
				return ErrSharedKey
			}
		}
	default:
		return ErrSharedNamespace
	}
	return nil
}

func (s *SharedV2) fail(err error) error {
	if err != nil && s.fault == nil {
		s.fault = err
		s.stats.Failures++
	}
	return err
}
func (s *SharedV2) checkOpen() error {
	if s.closed {
		return ErrClosed
	}
	return s.fault
}
func (s *SharedV2) refresh() error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	return s.fail(s.loadShared())
}

func (s *SharedV2) find(ns uint8, key []byte) (int, bool) {
	for i := 0; i < int(s.keyCount); i++ {
		e := &s.entries[i]
		if e.namespace > ns {
			return i, false
		}
		if e.namespace == ns {
			c := sharedCompare(s.keys[i][:e.keyLen], key)
			if c >= 0 {
				return i, c == 0
			}
		}
	}
	return int(s.keyCount), false
}

func (s *SharedV2) applyStaged() error {
	at, exists := s.find(s.staged.namespace, s.stagedKey[:s.staged.keyLen])
	oldCount := 0
	if exists {
		oldCount = int(s.entries[at].count)
	}
	newCount := int(s.staged.count)
	if !exists && newCount == 0 {
		return ErrCorrupt
	}
	if int(s.valueCount)-oldCount+newCount > sharedMaxValues {
		return ErrCapacity
	}
	if !exists && s.keyCount == sharedMaxEntries {
		return ErrCapacity
	}
	first := int(s.valueCount)
	if at < int(s.keyCount) {
		first = int(s.entries[at].first)
	}
	copy(s.values[first:], s.values[first+oldCount:s.valueCount])
	s.valueCount -= uint16(oldCount)
	for i := at + 1; i < int(s.keyCount); i++ {
		s.entries[i].first -= uint16(oldCount)
	}
	if newCount == 0 {
		for i := at; i < int(s.keyCount)-1; i++ {
			s.keys[i] = s.keys[i+1]
			s.entries[i] = s.entries[i+1]
		}
		s.keyCount--
	} else {
		if !exists {
			for i := int(s.keyCount); i > at; i-- {
				s.keys[i] = s.keys[i-1]
				s.entries[i] = s.entries[i-1]
			}
			s.keyCount++
		}
		copy(s.values[first+newCount:], s.values[first:s.valueCount])
		copy(s.values[first:first+newCount], s.stagedValues[:newCount])
		s.valueCount += uint16(newCount)
		for i := at + 1; i < int(s.keyCount); i++ {
			s.entries[i].first += uint16(newCount)
		}
		s.entries[at] = s.staged
		s.entries[at].first = uint16(first)
		s.keys[at] = s.stagedKey
	}
	return s.countShared()
}

func (s *SharedV2) countShared() error {
	var matterBytes, threadBytes uint32
	var matterKeys, threadKeys, threadValues uint16
	for i := 0; i < int(s.keyCount); i++ {
		e := &s.entries[i]
		switch e.namespace {
		case sharedMatter:
			if e.count != 1 {
				return ErrCorrupt
			}
			matterKeys++
			matterBytes += e.bytes
		case sharedThread:
			threadKeys++
			threadValues += e.count
			threadBytes += e.bytes
		default:
			return ErrSharedNamespace
		}
	}
	if matterKeys > 32 || threadKeys > 16 || threadValues > 32 || matterBytes > sharedMatterBytes || threadBytes > sharedThreadBytes {
		return ErrCapacity
	}
	s.stats.MatterEntryBytes, s.stats.ThreadEntryBytes = matterBytes, threadBytes
	s.stats.MatterKeys, s.stats.ThreadKeys, s.stats.ThreadValues = matterKeys, threadKeys, threadValues
	return nil
}
