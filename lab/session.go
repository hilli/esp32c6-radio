package lab

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type Command struct {
	Operation             string `json:"operation"`
	RunID                 string `json:"run_id"`
	BootID                string `json:"boot_id"`
	ExpectedEUI           string `json:"expected_eui64"`
	Confirmation          string `json:"confirmation"`
	Channel               uint8  `json:"channel"`
	PanID                 uint16 `json:"pan_id"`
	LocalAddress          uint16 `json:"local_address"`
	PeerAddress           uint16 `json:"peer_address"`
	Count                 uint32 `json:"count"`
	PeerIPv6              string `json:"peer_ipv6"`
	DurationSeconds       uint32 `json:"duration_seconds"`
	DatasetHex            string `json:"dataset_hex,omitempty"`
	SourceReviewArtifact  string `json:"source_review_artifact"`
	StorageReviewArtifact string `json:"storage_review_artifact"`
	EntropyReviewArtifact string `json:"entropy_review_artifact"`
	Counter               uint64 `json:"test_counter"`
	DelayMillis           uint32 `json:"delay_millis"`
}

func ParseCommand(p []byte) (Command, error) {
	var c Command
	if len(p) > 2048 {
		return c, errors.New("oversized command")
	}
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("trailing command")
	}
	return c, nil
}

type Session struct {
	FirmwareID, DeviceEUI string
	RunID, BootID         string
	Sequence              uint64
}

func (s *Session) Arm(c Command) error {
	if s.FirmwareID == "" || s.FirmwareID == "UNSET" || c.ExpectedEUI != s.DeviceEUI || c.RunID == "" || c.BootID == "" {
		return errors.New("explicit firmware/device/run/boot identity required")
	}
	if s.RunID != "" && (s.RunID != c.RunID || s.BootID != c.BootID) {
		return errors.New("identity change requires actual reboot/new session")
	}
	if s.RunID == "" && c.Operation != "hello" {
		return errors.New("hello identity handshake required")
	}
	s.RunID, s.BootID = c.RunID, c.BootID
	return nil
}
func (s *Session) Record(e Event) ([]byte, error) {
	s.Sequence++
	e.Version = Version
	e.Origin = "physical"
	e.FirmwareID = s.FirmwareID
	e.DeviceEUI = s.DeviceEUI
	e.RunID = s.RunID
	e.BootID = s.BootID
	e.Sequence = s.Sequence
	return Encode(e)
}
