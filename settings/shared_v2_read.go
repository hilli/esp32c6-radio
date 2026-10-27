package settings

import "math"

type sharedReader struct {
	owner       *SharedV2
	offset, end int
	crc         uint32
}

func (r *sharedReader) read(p []byte) error {
	if len(p) > r.end-r.offset {
		return ErrCorrupt
	}
	if err := r.owner.dev.read(p, r.offset); err != nil {
		return err
	}
	r.crc = sharedCRC(r.crc, p)
	r.offset += len(p)
	return nil
}
func (r *sharedReader) skip(n uint32) error {
	if uint64(n) > uint64(r.end-r.offset) {
		return ErrCorrupt
	}
	for n > 0 {
		count := min(n, uint32(len(r.owner.stream)))
		if err := r.read(r.owner.stream[:count]); err != nil {
			return err
		}
		n -= count
	}
	return nil
}

func (s *SharedV2) allSharedErased(off, n int) (bool, error) {
	for n > 0 {
		count := min(n, len(s.stream))
		if err := s.dev.read(s.stream[:count], off); err != nil {
			return false, err
		}
		if !sharedErased(s.stream[:count]) {
			return false, nil
		}
		off += count
		n -= count
	}
	return true, nil
}

func (s *SharedV2) readSharedValues(r *sharedReader, count uint16) (uint32, error) {
	if count > 32 {
		return 0, ErrCorrupt
	}
	var total uint32
	for i := 0; i < int(count); i++ {
		if err := r.read(s.header[:4]); err != nil {
			return 0, err
		}
		n := sharedU32(s.header[:], 0)
		limit := uint32(sharedThreadValue)
		if s.staged.namespace == sharedMatter {
			limit = sharedMatterValue
		}
		if n > limit {
			return 0, ErrCorrupt
		}
		s.stagedValues[i] = sharedValue{uint32(r.offset), n}
		if err := r.skip(n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func (s *SharedV2) readSharedSnapshot(bank int, b *sharedBank) error {
	s.keyCount, s.valueCount = 0, 0
	start := bank*SharedV2BankSize + sharedArenaStart
	r := sharedReader{owner: s, offset: start, end: start + int(b.snapshotBytes)}
	if err := r.read(s.header[:8]); err != nil {
		return err
	}
	keyCount, valueCount := sharedU16(s.header[:], 4), sharedU16(s.header[:], 6)
	if sharedU16(s.header[:], 0) != 1 || sharedU16(s.header[:], 2) != 0 || keyCount > 48 || valueCount > 64 {
		return ErrCorrupt
	}
	for i := 0; i < int(keyCount); i++ {
		if err := r.read(s.header[:8]); err != nil {
			return err
		}
		ns, keyLen, count := s.header[0], sharedU16(s.header[:], 2), sharedU16(s.header[:], 4)
		if s.header[1] != 0 || sharedU16(s.header[:], 6) != 0 || keyLen > 96 || count == 0 || count > 32 || (ns == sharedMatter && count != 1) {
			return ErrCorrupt
		}
		if err := r.read(s.stagedKey[:keyLen]); err != nil {
			return err
		}
		if err := sharedValidKey(ns, s.stagedKey[:keyLen]); err != nil {
			return err
		}
		if i > 0 {
			previous := &s.entries[i-1]
			if previous.namespace > ns || (previous.namespace == ns && sharedCompare(s.keys[i-1][:previous.keyLen], s.stagedKey[:keyLen]) >= 0) {
				return ErrCorrupt
			}
		}
		s.staged = sharedEntry{namespace: ns, keyLen: keyLen, count: count, source: uint32(start), generation: b.base}
		total, err := s.readSharedValues(&r, count)
		if err != nil {
			return err
		}
		s.staged.bytes = 8 + uint32(keyLen) + 4*uint32(count) + total
		if err := s.applyStaged(); err != nil {
			return ErrCorrupt
		}
	}
	if r.offset != r.end || r.crc != b.snapshotCRC || s.valueCount != valueCount {
		return ErrCorrupt
	}
	if ok, err := s.allSharedErased(r.end, int(sharedAlign(b.snapshotBytes)-b.snapshotBytes)); err != nil {
		return err
	} else if !ok {
		return ErrCorrupt
	}
	return nil
}

func (s *SharedV2) readSharedRecord(bank, slot int, b *sharedBank) error {
	d := s.descriptor[:]
	ns, op, keyLen := d[12], d[13], sharedU16(d, 14)
	off, n, expected := sharedU32(d, 16), sharedU32(d, 20), sharedU32(d, 24)
	gen := sharedU64(d, 4)
	if sharedU32(d, 0) != sharedRecordMagic || b.generation == math.MaxUint64 || gen != b.generation+1 ||
		keyLen == 0 || keyLen > 96 || off != b.tail || off%4 != 0 || n < uint32(keyLen)+4 || n > 16488 ||
		off > sharedRetire || sharedAlign(n) > sharedRetire-off || (op != sharedReplace && op != sharedDelete) {
		return ErrCorrupt
	}
	start := bank*SharedV2BankSize + int(off)
	r := sharedReader{owner: s, offset: start, end: start + int(n), crc: sharedCRC(0, d[:24])}
	if err := r.read(s.stagedKey[:keyLen]); err != nil {
		return err
	}
	if err := sharedValidKey(ns, s.stagedKey[:keyLen]); err != nil {
		return err
	}
	if err := r.read(s.header[:4]); err != nil {
		return err
	}
	count := sharedU16(s.header[:], 0)
	if sharedU16(s.header[:], 2) != 0 || count > 32 || (op == sharedDelete && count != 0) || (op == sharedReplace && count == 0) || (ns == sharedMatter && op == sharedReplace && count != 1) {
		return ErrCorrupt
	}
	s.staged = sharedEntry{namespace: ns, keyLen: keyLen, count: count, source: uint32(bank*SharedV2BankSize + sharedHeaderSize + slot*32), generation: gen}
	total, err := s.readSharedValues(&r, count)
	if err != nil {
		return err
	}
	if r.offset != r.end || r.crc != expected {
		return ErrCorrupt
	}
	s.staged.bytes = 8 + uint32(keyLen) + 4*uint32(count) + total
	if ok, err := s.allSharedErased(r.end, int(sharedAlign(n)-n)); err != nil {
		return err
	} else if !ok {
		return ErrCorrupt
	}
	if err := s.applyStaged(); err != nil {
		return ErrCorrupt
	}
	b.generation = gen
	b.tail += sharedAlign(n)
	b.slot++
	return nil
}

func (s *SharedV2) readSharedBank(bank int) (sharedBank, error) {
	return s.readSharedBankState(bank, false)
}

func (s *SharedV2) readSharedBankState(bank int, preparing bool) (sharedBank, error) {
	var b sharedBank
	base := bank * SharedV2BankSize
	if err := s.dev.read(s.descriptor[:4], base+sharedRetire); err != nil {
		return b, err
	}
	retired := sharedU32(s.descriptor[:], 0)
	if retired != 0 && retired != math.MaxUint32 {
		return b, ErrAmbiguous
	}
	if retired == 0 {
		b.retired = true
		return b, nil
	}
	if err := s.dev.read(s.header[:], base); err != nil {
		return b, err
	}
	h := s.header[:]
	bankCommit := sharedU32(h, 112)
	if bankCommit != 0 && bankCommit != math.MaxUint32 {
		return b, ErrAmbiguous
	}
	if bankCommit == math.MaxUint32 && !preparing {
		return b, nil
	}
	if preparing && bankCommit != math.MaxUint32 {
		return b, ErrCorrupt
	}
	if string(h[:8]) != "C6SHR002" || sharedU16(h, 8) != 2 || sharedU16(h, 10) != 128 || sharedU32(h, 12) != 1 {
		return b, ErrSharedProfile
	}
	if sharedU32(h, 16) != SharedV2PartitionStart || sharedU32(h, 20) != SharedV2PartitionSize || sharedU32(h, 24) != SharedV2BankSize ||
		sharedU16(h, 28) != 128 || sharedU16(h, 30) != 32 ||
		sharedU32(h, 72) != 65536 || sharedU32(h, 76) != 4096 || sharedU32(h, 80) != 16384 ||
		sharedU16(h, 84) != 96 || sharedU16(h, 86) != 32 || sharedU16(h, 88) != 16 || sharedU16(h, 90) != 32 ||
		sharedU16(h, 92) != 512 || sharedU16(h, 94) != 32 || sharedU16(h, 96) != 1 || sharedU16(h, 98) != 1 ||
		sharedU32(h, 100) != 4096 || sharedU16(h, 104) != 64 || sharedU16(h, 106) != 0 {
		return b, ErrSharedProfile
	}
	if sharedCRC(0, h[:108]) != sharedU32(h, 108) || !sharedErased(h[116:128]) {
		return b, ErrCorrupt
	}
	for i := 0; i < 8; i++ {
		if h[56+i] != s.eui[i] {
			return b, ErrOwner
		}
	}
	b.epoch, b.base, b.parent = sharedU64(h, 32), sharedU64(h, 40), sharedU64(h, 48)
	if b.epoch == 0 || (b.epoch == 1 && (b.parent != 0 || b.base != 0)) || (b.epoch > 1 && b.parent != b.epoch-1) {
		return b, ErrCorrupt
	}
	b.generation = b.base
	b.snapshotBytes, b.snapshotCRC = sharedU32(h, 64), sharedU32(h, 68)
	if b.snapshotBytes < 8 || b.snapshotBytes > sharedMaxSnapshot {
		return b, ErrCorrupt
	}
	b.tail = sharedArenaStart + sharedAlign(b.snapshotBytes)
	if err := s.readSharedSnapshot(bank, &b); err != nil {
		return b, err
	}
	tailSeen := false
	for slot := 0; slot < sharedSlots; slot++ {
		if err := s.dev.read(s.descriptor[:], base+sharedHeaderSize+slot*32); err != nil {
			return b, err
		}
		committed := sharedU32(s.descriptor[:], 28)
		if committed != 0 && committed != math.MaxUint32 {
			return b, ErrAmbiguous
		}
		if committed == math.MaxUint32 {
			tailSeen = true
			if !sharedErased(s.descriptor[:]) {
				b.dirty = true
			}
			continue
		}
		if tailSeen {
			return b, ErrCorrupt
		}
		if err := s.readSharedRecord(bank, slot, &b); err != nil {
			return b, err
		}
	}
	if ok, err := s.allSharedErased(base+int(b.tail), sharedRetire-int(b.tail)); err != nil {
		return b, err
	} else if !ok {
		b.dirty = true
	}
	b.valid = true
	return b, nil
}

func (s *SharedV2) loadShared() error {
	for i := 0; i < 2; i++ {
		b, err := s.readSharedBank(i)
		if err != nil {
			return err
		}
		s.banks[i] = b
	}
	a, b := &s.banks[0], &s.banks[1]
	s.retirePending = -1
	switch {
	case !a.valid && !b.valid:
		ok, err := s.allSharedErased(0, SharedV2PartitionSize)
		if err != nil {
			return err
		}
		if ok {
			return ErrUnformatted
		}
		return ErrCorrupt
	case a.valid && !b.valid:
		s.active = 0
	case b.valid && !a.valid:
		s.active = 1
	default:
		s.active = 0
		if b.epoch > a.epoch {
			s.active = 1
		}
		newer, older := &s.banks[s.active], &s.banks[1-s.active]
		if older.epoch == math.MaxUint64 || newer.epoch != older.epoch+1 || newer.parent != older.epoch ||
			newer.base != older.generation || older.generation == math.MaxUint64 || newer.generation != older.generation+1 {
			return ErrCorrupt
		}
		s.retirePending = 1 - s.active
	}
	current, err := s.readSharedBank(int(s.active))
	if err != nil {
		return err
	}
	if !current.valid {
		return ErrCorrupt
	}
	s.banks[s.active] = current
	s.stats.Epoch, s.stats.Generation = current.epoch, current.generation
	return s.countShared()
}
