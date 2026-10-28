package image

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type sharedManifest struct {
	Version      int    `json:"version"`
	Profile      string `json:"profile"`
	Schema       int    `json:"schema"`
	ProfileID    int    `json:"profile_id"`
	FlashBytes   int    `json:"flash_bytes"`
	Start        int    `json:"partition_start"`
	Bytes        int    `json:"partition_bytes"`
	Bank         int    `json:"bank_bytes"`
	Header       int    `json:"header_bytes"`
	Slots        int    `json:"directory_slots"`
	Descriptor   int    `json:"descriptor_bytes"`
	Arena        int    `json:"arena_start"`
	Retire       int    `json:"retirement_offset"`
	MatterBytes  int    `json:"matter_entry_bytes"`
	ThreadBytes  int    `json:"thread_entry_bytes"`
	MatterKeys   int    `json:"matter_keys"`
	ThreadKeys   int    `json:"thread_keys"`
	MatterValues int    `json:"matter_values"`
	ThreadValues int    `json:"thread_values"`
	KeyBytes     int    `json:"matter_key_bytes"`
	MatterValue  int    `json:"matter_value_bytes"`
	ThreadValue  int    `json:"thread_value_bytes"`
	SmallPayload int    `json:"small_payload_bytes"`
	SmallSlots   int    `json:"small_write_slots_reserved"`
	SmallArena   int    `json:"small_write_arena_reserved"`
	Physical     string `json:"physical_status"`
}

// CheckSharedV2Manifest validates an exact closed profile, not caller-supplied
// geometry or a physical qualification. Duplicate JSON fields are rejected.
func CheckSharedV2Manifest(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8193))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid shared profile manifest")
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return fmt.Errorf("invalid manifest key")
		}
		if _, duplicate := fields[key]; duplicate {
			return fmt.Errorf("duplicate manifest field")
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		fields[key] = raw
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing manifest data")
	}
	p, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var got sharedManifest
	decoder := json.NewDecoder(bytes.NewReader(p))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		return err
	}
	want := sharedManifest{1, "c6-shared-v2-lab1", 2, 1, 4194304, 0x3d0000, 196608, 98304, 128, 128, 32, 4224, 98300, 65536, 4096, 32, 16, 32, 32, 96, 16384, 512, 64, 64, 4096, "NOT_RUN"}
	if got != want || len(fields) != 26 {
		return fmt.Errorf("wrong/incomplete c6-shared-v2-lab1 manifest")
	}
	return nil
}
