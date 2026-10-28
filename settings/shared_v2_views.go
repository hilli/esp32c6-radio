package settings

import (
	"strings"
	"unicode/utf8"

	"github.com/hilli/gothread/platform"
)

type SharedThreadSettings struct {
	owner  *SharedV2
	closed bool
}
type SharedMatterBlobs struct {
	owner  *SharedV2
	closed bool
}

var _ platform.Settings = (*SharedThreadSettings)(nil)

func (s *SharedV2) ThreadSettings() platform.Settings { return &s.thread }
func (s *SharedV2) MatterBlobs() *SharedMatterBlobs   { return &s.matter }

func (s *SharedV2) stageSharedKey(ns uint8, key []byte) error {
	if err := sharedValidKey(ns, key); err != nil {
		return err
	}
	copy(s.stagedKey[:], key)
	s.staged = sharedEntry{namespace: ns, keyLen: uint16(len(key))}
	s.pendingOp = sharedReplace
	s.borrowed = nil
	return nil
}
func sharedValidString(key string) bool {
	return len(key) > 0 && len(key) <= 96 && utf8.ValidString(key) && !strings.ContainsRune(key, 0)
}

func (s *SharedV2) getShared(ns uint8, key []byte, index int, missing error) ([]byte, error) {
	at, exists := s.find(ns, key)
	if !exists || index >= int(s.entries[at].count) {
		return nil, missing
	}
	v := s.values[int(s.entries[at].first)+index]
	out := make([]byte, v.length)
	if err := s.dev.read(out, int(v.offset)); err != nil {
		return nil, s.fail(err)
	}
	// The output is not published until its committed backing is revalidated.
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *SharedThreadSettings) Get(key platform.SettingsKey, index int) ([]byte, error) {
	if v == nil || v.owner == nil {
		return nil, ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return nil, ErrClosed
	}
	if index < 0 {
		return nil, ErrIndex
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	var k [2]byte
	sharedPut16(k[:], 0, uint16(key))
	return s.getShared(sharedThread, k[:], index, platform.ErrSettingsNotFound)
}

func (v *SharedThreadSettings) Set(key platform.SettingsKey, data []byte) error {
	return v.replace(key, data, false)
}
func (v *SharedThreadSettings) Add(key platform.SettingsKey, data []byte) error {
	return v.replace(key, data, true)
}
func (v *SharedThreadSettings) replace(key platform.SettingsKey, data []byte, add bool) error {
	if v == nil || v.owner == nil {
		return ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if err := s.refresh(); err != nil {
		return err
	}
	if len(data) > 512 {
		return ErrCapacity
	}
	var k [2]byte
	sharedPut16(k[:], 0, uint16(key))
	if err := s.stageSharedKey(sharedThread, k[:]); err != nil {
		return err
	}
	count := 0
	if at, ok := s.find(sharedThread, k[:]); ok && add {
		e := &s.entries[at]
		count = int(e.count)
		if count == 32 {
			return ErrCapacity
		}
		copy(s.stagedValues[:count], s.values[e.first:int(e.first)+count])
	}
	s.borrowed = data
	s.stagedValues[count] = sharedValue{sharedCallerOffset, uint32(len(data))}
	s.staged.count = uint16(count + 1)
	return s.mutateShared()
}

func (v *SharedThreadSettings) Delete(key platform.SettingsKey, index int) error {
	if v == nil || v.owner == nil {
		return ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if index < -1 {
		return ErrIndex
	}
	if err := s.refresh(); err != nil {
		return err
	}
	var k [2]byte
	sharedPut16(k[:], 0, uint16(key))
	at, exists := s.find(sharedThread, k[:])
	if !exists || index >= int(s.entries[at].count) {
		return platform.ErrSettingsNotFound
	}
	e := s.entries[at]
	if err := s.stageSharedKey(sharedThread, k[:]); err != nil {
		return err
	}
	if index >= 0 {
		for i := 0; i < int(e.count); i++ {
			if i == index {
				continue
			}
			s.stagedValues[s.staged.count] = s.values[int(e.first)+i]
			s.staged.count++
		}
	}
	if s.staged.count == 0 {
		s.pendingOp = sharedDelete
	}
	return s.mutateShared()
}
func (v *SharedThreadSettings) Wipe() error {
	if v == nil || v.owner == nil {
		return ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if err := s.checkOpen(); err != nil {
		return err
	}
	return ErrSharedScope
}
func (v *SharedThreadSettings) Close() error {
	if v == nil || v.owner == nil {
		return nil
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	v.closed = true
	return nil
}

func (v *SharedMatterBlobs) Get(key string) ([]byte, error) {
	if v == nil || v.owner == nil {
		return nil, ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return nil, ErrClosed
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	if !sharedValidString(key) {
		return nil, ErrSharedKey
	}
	copy(s.stagedKey[:], key)
	return s.getShared(sharedMatter, s.stagedKey[:len(key)], 0, ErrBlobNotFound)
}
func (v *SharedMatterBlobs) Set(key string, data []byte) error {
	if v == nil || v.owner == nil {
		return ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if err := s.refresh(); err != nil {
		return err
	}
	if !sharedValidString(key) {
		return ErrSharedKey
	}
	if len(data) > sharedMatterValue {
		return ErrCapacity
	}
	copy(s.stagedKey[:], key)
	s.staged = sharedEntry{namespace: sharedMatter, keyLen: uint16(len(key)), count: 1}
	s.pendingOp = sharedReplace
	s.borrowed = data
	s.stagedValues[0] = sharedValue{sharedCallerOffset, uint32(len(data))}
	return s.mutateShared()
}
func (v *SharedMatterBlobs) Delete(key string) error {
	if v == nil || v.owner == nil {
		return ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return ErrClosed
	}
	if err := s.refresh(); err != nil {
		return err
	}
	if !sharedValidString(key) {
		return ErrSharedKey
	}
	copy(s.stagedKey[:], key)
	if _, ok := s.find(sharedMatter, s.stagedKey[:len(key)]); !ok {
		return ErrBlobNotFound
	}
	s.staged = sharedEntry{namespace: sharedMatter, keyLen: uint16(len(key))}
	s.pendingOp = sharedDelete
	s.borrowed = nil
	return s.mutateShared()
}
func (v *SharedMatterBlobs) Keys(prefix string) ([]string, error) {
	if v == nil || v.owner == nil {
		return nil, ErrClosed
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.closed {
		return nil, ErrClosed
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	count := 0
	for i := 0; i < int(s.keyCount); i++ {
		e := &s.entries[i]
		if e.namespace == sharedMatter && sharedPrefix(s.keys[i][:e.keyLen], prefix) {
			count++
		}
	}
	out := make([]string, 0, count)
	for i := 0; i < int(s.keyCount); i++ {
		e := &s.entries[i]
		if e.namespace == sharedMatter && sharedPrefix(s.keys[i][:e.keyLen], prefix) {
			out = append(out, string(s.keys[i][:e.keyLen]))
		}
	}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return out, nil
}
func (v *SharedMatterBlobs) Close() error {
	if v == nil || v.owner == nil {
		return nil
	}
	s := v.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	v.closed = true
	return nil
}

func sharedPrefix(key []byte, prefix string) bool {
	if len(prefix) > len(key) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if key[i] != prefix[i] {
			return false
		}
	}
	return true
}
