package lab

import "fmt"

// Witness references an independently captured sniffer/reference-peer artifact.
// It contains no datasets, network keys or raw entropy.
type Witness struct {
	Version            int    `json:"version"`
	Origin             string `json:"origin"`
	RunID              string `json:"run_id"`
	DeviceEUI          string `json:"device_eui64"`
	FirmwareID         string `json:"firmware_id"`
	Kind               string `json:"kind"`
	ArtifactSHA256     string `json:"artifact_sha256"`
	Channel            uint8  `json:"channel"`
	Frames             uint64 `json:"frames"`
	ValidFCS           uint64 `json:"valid_fcs"`
	MaxACKTurnaroundUS uint64 `json:"max_ack_turnaround_us"`
	EchoReplies        uint64 `json:"echo_replies"`
}

func ApplyWitness(m Manifest, r *Report, w Witness, actualHash string) error {
	if w.Version != Version || w.Origin != "physical" || r.Origin != "physical" || w.RunID != m.RunID || w.DeviceEUI != m.ExpectedEUI || w.FirmwareID != m.FirmwareID || len(w.ArtifactSHA256) != 64 || w.ArtifactSHA256 != actualHash {
		return fmt.Errorf("independent witness identity/artifact does not match")
	}
	switch w.Kind {
	case "sniffer":
		if uint64(w.Channel) != r.RFChannel || w.Channel < 11 || w.Channel > 26 || w.Frames < 1000 || w.ValidFCS < w.Frames || w.MaxACKTurnaroundUS == 0 || w.MaxACKTurnaroundUS > 192 {
			r.Checks["rf_timing_fcs"] = Fail
			r.Checks["rf"] = Fail
			return fmt.Errorf("RF/FCS/ACK timing evidence failed")
		}
		if r.Checks["rf_timing_fcs"] == Fail {
			return fmt.Errorf("prior RF witness failed")
		}
		r.Checks["rf_timing_fcs"] = Pass
		if r.Checks["rf_counters"] == Pass && r.Checks["identity"] == Pass && r.Checks["rf"] != Fail {
			r.Checks["rf"] = Pass
		}
	case "reference-peer":
		if w.EchoReplies == 0 {
			return fmt.Errorf("no independently observed echo replies")
		}
		if r.Checks["thread_device"] == Pass && r.Checks["thread_counter_safety"] == Pass && r.Checks["identity"] == Pass && r.Checks["thread"] != Fail {
			r.Checks["thread"] = Pass
		}
	default:
		return fmt.Errorf("unknown independent witness kind")
	}
	return nil
}
