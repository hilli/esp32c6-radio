package settings

import (
	"math"
	"unsafe"
)

type sharedWriter struct {
	owner  *SharedV2
	offset int
	count  uint32
	crc    uint32
	used   int
	emit   bool
}

func (s *SharedV2) pageBytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&s.page[0])), 256)
}
func (w *sharedWriter) space() int {
	if !w.emit {
		return 256
	}
	return 256 - (w.offset & 255) - w.used
}
func (w *sharedWriter) flush() error {
	if w.used == 0 {
		return nil
	}
	p := w.owner.pageBytes()
	n := int(sharedAlign(uint32(w.used)))
	for i := w.used; i < n; i++ {
		p[i] = 255
	}
	w.owner.stats.ProgramCalls++
	w.owner.stats.ProgramBytes += uint64(n)
	if err := w.owner.dev.program(p[:n], w.offset); err != nil {
		return err
	}
	if err := w.owner.dev.read(w.owner.stream[:n], w.offset); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		if w.owner.stream[i] != p[i] {
			return ErrCorrupt
		}
	}
	w.offset += n
	w.used = 0
	return nil
}
func (w *sharedWriter) write(p []byte) error {
	w.crc = sharedCRC(w.crc, p)
	w.count += uint32(len(p))
	if !w.emit {
		return nil
	}
	for len(p) > 0 {
		n := min(w.space(), len(p))
		copy(w.owner.pageBytes()[w.used:w.used+n], p[:n])
		w.used += n
		p = p[n:]
		if w.space() == 0 {
			if err := w.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *SharedV2) writeSharedBytes(off int, p []byte) error {
	w := sharedWriter{owner: s, offset: off, emit: true}
	if err := w.write(p); err != nil {
		return err
	}
	return w.flush()
}
func (s *SharedV2) writeSharedZero(off int) error {
	for i := 0; i < 4; i++ {
		s.header[i] = 0
	}
	return s.writeSharedBytes(off, s.header[:4])
}

func (s *SharedV2) streamSharedValue(w *sharedWriter, v sharedValue) error {
	if v.offset == sharedCallerOffset {
		if int(v.length) != len(s.borrowed) {
			return ErrCorrupt
		}
		return w.write(s.borrowed)
	}
	left, off := v.length, v.offset
	for left > 0 {
		// flush readback shares stream; consume each flash chunk completely
		// before a flush can overwrite that chunk.
		n := min(left, uint32(w.space()))
		if err := s.dev.read(s.stream[:n], int(off)); err != nil {
			return err
		}
		if err := w.write(s.stream[:n]); err != nil {
			return err
		}
		off += n
		left -= n
	}
	return nil
}

func (s *SharedV2) streamSharedSnapshot(w *sharedWriter) error {
	h := s.header[:8]
	clear(h)
	sharedPut16(h, 0, 1)
	sharedPut16(h, 4, s.keyCount)
	sharedPut16(h, 6, s.valueCount)
	if err := w.write(h); err != nil {
		return err
	}
	for i := 0; i < int(s.keyCount); i++ {
		e := &s.entries[i]
		clear(h)
		h[0] = e.namespace
		sharedPut16(h, 2, e.keyLen)
		sharedPut16(h, 4, e.count)
		if err := w.write(h); err != nil {
			return err
		}
		if err := w.write(s.keys[i][:e.keyLen]); err != nil {
			return err
		}
		for j := 0; j < int(e.count); j++ {
			v := s.values[int(e.first)+j]
			sharedPut32(h, 0, v.length)
			if err := w.write(h[:4]); err != nil {
				return err
			}
			if err := s.streamSharedValue(w, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SharedV2) streamSharedPayload(w *sharedWriter) error {
	if err := w.write(s.stagedKey[:s.staged.keyLen]); err != nil {
		return err
	}
	clear(s.header[:4])
	sharedPut16(s.header[:], 0, s.staged.count)
	if err := w.write(s.header[:4]); err != nil {
		return err
	}
	for i := 0; i < int(s.staged.count); i++ {
		v := s.stagedValues[i]
		sharedPut32(s.header[:], 0, v.length)
		if err := w.write(s.header[:4]); err != nil {
			return err
		}
		if err := s.streamSharedValue(w, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *SharedV2) sharedHeader(epoch, base, parent uint64, n, crc uint32) {
	h := s.header[:]
	for i := range h {
		h[i] = 255
	}
	copy(h, "C6SHR002")
	sharedPut16(h, 8, 2)
	sharedPut16(h, 10, 128)
	sharedPut32(h, 12, 1)
	sharedPut32(h, 16, SharedV2PartitionStart)
	sharedPut32(h, 20, SharedV2PartitionSize)
	sharedPut32(h, 24, SharedV2BankSize)
	sharedPut16(h, 28, 128)
	sharedPut16(h, 30, 32)
	sharedPut64(h, 32, epoch)
	sharedPut64(h, 40, base)
	sharedPut64(h, 48, parent)
	copy(h[56:64], s.eui[:])
	sharedPut32(h, 64, n)
	sharedPut32(h, 68, crc)
	sharedPut32(h, 72, 65536)
	sharedPut32(h, 76, 4096)
	sharedPut32(h, 80, 16384)
	sharedPut16(h, 84, 96)
	sharedPut16(h, 86, 32)
	sharedPut16(h, 88, 16)
	sharedPut16(h, 90, 32)
	sharedPut16(h, 92, 512)
	sharedPut16(h, 94, 32)
	sharedPut16(h, 96, 1)
	sharedPut16(h, 98, 1)
	sharedPut32(h, 100, 4096)
	sharedPut16(h, 104, 64)
	sharedPut16(h, 106, 0)
	sharedPut32(h, 108, sharedCRC(0, h[:108]))
}

func (s *SharedV2) writeSharedRecord(bank, slot int, tail uint32, generation uint64) error {
	d := s.descriptor[:]
	for i := range d {
		d[i] = 255
	}
	sharedPut32(d, 0, sharedRecordMagic)
	sharedPut64(d, 4, generation)
	d[12], d[13] = s.staged.namespace, s.pendingOp
	sharedPut16(d, 14, s.staged.keyLen)
	sharedPut32(d, 16, tail)
	sharedPut32(d, 20, s.payloadBytes)
	seed := sharedCRC(0, d[:24])
	first := sharedWriter{owner: s, crc: seed}
	if err := s.streamSharedPayload(&first); err != nil {
		return err
	}
	if first.count != s.payloadBytes {
		return ErrCorrupt
	}
	sharedPut32(d, 24, first.crc)
	directory := bank*SharedV2BankSize + sharedHeaderSize + slot*32
	if err := s.writeSharedBytes(directory, d[:28]); err != nil {
		return err
	}
	second := sharedWriter{owner: s, offset: bank*SharedV2BankSize + int(tail), emit: true, crc: seed}
	if err := s.streamSharedPayload(&second); err != nil {
		return err
	}
	if err := second.flush(); err != nil {
		return err
	}
	if second.count != first.count || second.crc != first.crc {
		return ErrCorrupt
	}
	return s.writeSharedZero(directory + 28)
}

func (s *SharedV2) preflightShared() error {
	b := &s.banks[s.active]
	if b.generation == math.MaxUint64 {
		return ErrExhausted
	}
	if err := sharedValidKey(s.staged.namespace, s.stagedKey[:s.staged.keyLen]); err != nil {
		return err
	}
	if s.staged.count > 32 {
		return ErrCapacity
	}
	at, exists := s.find(s.staged.namespace, s.stagedKey[:s.staged.keyLen])
	var oldBytes uint32
	var oldValues uint16
	if exists {
		oldBytes = s.entries[at].bytes
		oldValues = s.entries[at].count
	}
	var payload uint32
	for i := 0; i < int(s.staged.count); i++ {
		limit := uint32(sharedThreadValue)
		if s.staged.namespace == sharedMatter {
			limit = sharedMatterValue
		}
		if s.stagedValues[i].length > limit {
			return ErrCapacity
		}
		payload += s.stagedValues[i].length
	}
	s.staged.bytes = 8 + uint32(s.staged.keyLen) + 4*uint32(s.staged.count) + payload
	newBytes := s.staged.bytes
	if s.staged.count == 0 {
		newBytes = 0
	}
	keysDelta := int32(0)
	if !exists {
		keysDelta = 1
	}
	if s.staged.count == 0 {
		keysDelta = -1
	}
	if s.staged.namespace == sharedMatter {
		if s.staged.count > 1 || int32(s.stats.MatterKeys)+keysDelta > 32 ||
			s.stats.MatterEntryBytes-oldBytes+newBytes > sharedMatterBytes {
			return ErrCapacity
		}
	} else {
		if s.staged.count > 32 || int32(s.stats.ThreadKeys)+keysDelta > 16 ||
			uint32(s.stats.ThreadValues)-uint32(oldValues)+uint32(s.staged.count) > 32 ||
			s.stats.ThreadEntryBytes-oldBytes+newBytes > sharedThreadBytes {
			return ErrCapacity
		}
	}
	s.payloadBytes = uint32(s.staged.keyLen) + 4 + 4*uint32(s.staged.count) + payload
	need := sharedAlign(s.payloadBytes)
	slots := uint32(1)
	if s.payloadBytes > sharedSmallPayload {
		need += sharedHotBytes
		slots += sharedHotSlots
	}
	s.compact = b.dirty || need > sharedRetire-b.tail || slots > sharedSlots-uint32(b.slot)
	if s.compact && b.epoch == math.MaxUint64 {
		return ErrExhausted
	}
	return nil
}

func (s *SharedV2) mutateShared() error {
	if err := s.preflightShared(); err != nil {
		s.borrowed = nil
		return err
	}
	s.began = false
	err := s.dev.transaction(s.work)
	s.borrowed = nil
	if err != nil && s.began {
		return s.fail(err)
	}
	return err
}

func (s *SharedV2) commitWork() error {
	s.began = true
	active := int(s.active)
	b := s.banks[active]
	if s.retirePending >= 0 {
		if err := s.writeSharedZero(int(s.retirePending)*SharedV2BankSize + sharedRetire); err != nil {
			return err
		}
	}
	if !s.compact {
		if err := s.writeSharedRecord(active, int(b.slot), b.tail, b.generation+1); err != nil {
			return err
		}
		return s.loadShared()
	}
	first := sharedWriter{owner: s}
	if err := s.streamSharedSnapshot(&first); err != nil {
		return err
	}
	if first.count != 8+s.stats.MatterEntryBytes+s.stats.ThreadEntryBytes || first.count > sharedMaxSnapshot {
		return ErrCorrupt
	}
	target := 1 - active
	base := target * SharedV2BankSize
	for sector := 0; sector < SharedV2BankSize/SectorSize; sector++ {
		s.stats.EraseSectors++
		if err := s.dev.eraseSector(base + sector*SectorSize); err != nil {
			return err
		}
	}
	s.sharedHeader(b.epoch+1, b.generation, b.epoch, first.count, first.crc)
	if err := s.writeSharedBytes(base, s.header[:]); err != nil {
		return err
	}
	second := sharedWriter{owner: s, offset: base + sharedArenaStart, emit: true}
	if err := s.streamSharedSnapshot(&second); err != nil {
		return err
	}
	if err := second.flush(); err != nil {
		return err
	}
	if first.count != second.count || first.crc != second.crc {
		return ErrCorrupt
	}
	s.stats.CopiedBytes += uint64(second.count)
	if err := s.writeSharedRecord(target, 0, sharedArenaStart+sharedAlign(second.count), b.generation+1); err != nil {
		return err
	}
	candidate, err := s.readSharedBankState(target, true)
	if err != nil {
		return err
	}
	if !candidate.valid || candidate.epoch != b.epoch+1 || candidate.base != b.generation || candidate.generation != b.generation+1 {
		return ErrCorrupt
	}
	if err := s.writeSharedZero(base + 112); err != nil {
		return err
	}
	candidate, err = s.readSharedBank(target)
	if err != nil {
		return err
	}
	if !candidate.valid {
		return ErrCorrupt
	}
	if err := s.writeSharedZero(active*SharedV2BankSize + sharedRetire); err != nil {
		return err
	}
	s.stats.Compactions++
	return s.loadShared()
}

// Initial creation is explicit, never an Open/recovery side effect. Even the
// unused tail is checked; erased headers do not hide legacy schema1 data.
func formatSharedV2(dev sharedMedium, eui [8]byte) error {
	if eui == [8]byte{} {
		return ErrOwner
	}
	if err := dev.closeAllowed(); err != nil {
		return err
	}
	s := &SharedV2{dev: dev, eui: eui}
	ok, err := s.allSharedErased(0, SharedV2PartitionSize)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCorrupt
	}
	return dev.transaction(s.initializeSharedWork)
}
func (s *SharedV2) initializeSharedWork() error {
	if err := s.dev.closeAllowed(); err != nil {
		return err
	}
	first := sharedWriter{owner: s}
	if err := s.streamSharedSnapshot(&first); err != nil {
		return err
	}
	s.sharedHeader(1, 0, 0, first.count, first.crc)
	if err := s.writeSharedBytes(0, s.header[:]); err != nil {
		return err
	}
	second := sharedWriter{owner: s, offset: sharedArenaStart, emit: true}
	if err := s.streamSharedSnapshot(&second); err != nil {
		return err
	}
	if err := second.flush(); err != nil {
		return err
	}
	if first.count != second.count || first.crc != second.crc {
		return ErrCorrupt
	}
	if _, err := s.readSharedBankState(0, true); err != nil {
		return err
	}
	if err := s.writeSharedZero(112); err != nil {
		return err
	}
	return s.loadShared()
}
