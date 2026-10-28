package settings

import (
	"bytes"
	"testing"
)

func TestSharedV2RefusesMeasuredConsumerBlobBeforeMutation(t *testing.T) {
	// Consumer source 0d3dd54fc1ccefd090dd0b329d9ce787abac523d,
	// storage-activation-attempt1.json: 19 keys, 87357 encoded Matter entry
	// bytes, 22996 largest blob. These are refusal inputs, NOT admission.
	const measuredBlob = 22996
	const measuredLive = 87357
	s, f := sharedFixture(t)
	limits := s.Limits()
	if limits.MatterValueBytes >= measuredBlob || limits.MatterEntryBytes >= measuredLive {
		t.Fatal("experimental profile was silently enlarged")
	}
	if err := s.ThreadSettings().Set(65535, value(100)); err != nil {
		t.Fatal(err)
	}
	if err := s.MatterBlobs().Set("groupkeys/250", []byte("last acknowledged")); err != nil {
		t.Fatal(err)
	}
	before := f.steps
	if err := s.MatterBlobs().Set("groupkeys/250", make([]byte, measuredBlob)); err != ErrCapacity || f.steps != before {
		t.Fatal("oversized consumer blob mutated storage", err)
	}
	if s.fault != nil {
		t.Fatal("definite preflight refusal latched a valid owner")
	}
	requireSharedBlob(t, s, "groupkeys/250", []byte("last acknowledged"))
	p, err := s.ThreadSettings().Get(65535, 0)
	if err != nil || !bytes.Equal(p, value(100)) {
		t.Fatal("refusal changed Thread reservation", err)
	}
	// Aggregate admission and multi-key atomicity belong to the consumer.
	// This backend does not split the 22996-byte value into fitting records.
}
