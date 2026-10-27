// Package lab defines bounded, versioned laboratory evidence. Synthetic checks
// are never promoted to physical PASS, and missing evidence never means success.
package lab

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Version = 1
const Prefix = "C6EVIDENCE "

type Status string

const (
	Pass   Status = "PASS"
	Fail   Status = "FAIL"
	NotRun Status = "NOT_RUN"
)

type Event struct {
	Version    int               `json:"version"`
	Origin     string            `json:"origin"`
	RunID      string            `json:"run_id"`
	BootID     string            `json:"boot_id"`
	FirmwareID string            `json:"firmware_id"`
	DeviceEUI  string            `json:"device_eui64"`
	Probe      string            `json:"probe"`
	Stage      string            `json:"stage"`
	Status     Status            `json:"status"`
	Sequence   uint64            `json:"sequence"`
	Generation uint64            `json:"generation,omitempty"`
	Counter    uint64            `json:"test_counter,omitempty"`
	Metrics    map[string]uint64 `json:"metrics,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Digest     string            `json:"digest,omitempty"`
}

type Manifest struct {
	Version           int      `json:"version"`
	RunID             string   `json:"run_id"`
	FirmwareID        string   `json:"firmware_id"`
	Commit            string   `json:"commit"`
	Board             string   `json:"board_model"`
	ExpectedEUI       string   `json:"expected_eui64"`
	ChipRevision      string   `json:"chip_revision"`
	FlashID           string   `json:"flash_id"`
	GoVersion         string   `json:"go_version"`
	TinyGoVersion     string   `json:"tinygo_version"`
	ELFSHA256         string   `json:"elf_sha256"`
	BinSHA256         string   `json:"bin_sha256"`
	NativeRevision    string   `json:"native_revision"`
	SourceFingerprint string   `json:"source_fingerprint"`
	Scope             []string `json:"approved_scope"`
	Probe             string   `json:"probe"`
}

type Report struct {
	Version                    int               `json:"version"`
	RunID                      string            `json:"run_id"`
	Origin                     string            `json:"origin"`
	Checks                     map[string]Status `json:"checks"`
	Reasons                    []string          `json:"reasons"`
	LastAcknowledgedGeneration uint64            `json:"last_acknowledged_generation"`
	LastAcknowledgedCounter    uint64            `json:"last_acknowledged_test_counter"`
	Events                     int               `json:"events"`
	DeviceEUI                  string            `json:"device_eui64"`
	FirmwareID                 string            `json:"firmware_id"`
	LastMACCounter             uint64            `json:"last_mac_counter"`
	LastMLECounter             uint64            `json:"last_mle_counter"`
	RFChannel                  uint64            `json:"rf_channel"`
}

func Parse(line []byte) (Event, error) {
	var e Event
	if !bytes.HasPrefix(line, []byte(Prefix)) {
		return e, errors.New("not an evidence record")
	}
	if len(line) > 8192 {
		return e, errors.New("oversized evidence record")
	}
	d := json.NewDecoder(bytes.NewReader(line[len(Prefix):]))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return e, errors.New("trailing evidence data")
	}
	if e.Version != Version || (e.Origin != "physical" && e.Origin != "synthetic") ||
		(e.Status != Pass && e.Status != Fail && e.Status != NotRun) ||
		e.RunID == "" || e.BootID == "" || e.FirmwareID == "" || len(e.DeviceEUI) != 16 || e.Probe == "" || e.Stage == "" {
		return e, errors.New("incomplete or unsupported evidence identity")
	}
	if len(e.Reason) > 256 || strings.ContainsAny(e.Reason, "\r\n") {
		return e, errors.New("invalid reason")
	}
	for name := range e.Metrics {
		if !allowedMetric(name) {
			return e, errors.New("unknown/possibly secret metric field")
		}
	}
	return e, nil
}

func allowedMetric(name string) bool {
	switch name {
	case "offset", "bytes", "samples", "ones", "longest_byte_run", "repeated_words",
		"phy_active", "front_end_clocks", "rng_clock", "cpu_mhz", "apb_mhz",
		"accepted", "started", "terminal", "success", "noack", "cca", "abort", "rx_unique", "rx_duplicates", "rx_overflow", "rx_invalid", "rx_unsupported",
		"channel", "rssi_min_plus128", "rssi_max_plus128", "lqi_min", "lqi_max", "sniffer_ack_timing_artifact",
		"role_child", "secured_tx", "echo_in", "echo_out", "mac_counter", "mle_counter", "heap_bytes", "heap_high_water", "flash_program_bytes",
		"flash_erase_sectors", "radio_pause_microseconds", "reboot_counter_checks", "peer_echo_artifact",
		"duration_seconds", "heap_start", "heap_end", "bounded_memory", "detached_samples":
		return true
	}
	return false
}

func Evaluate(m Manifest, events []Event) Report {
	r := Report{Version: Version, RunID: m.RunID, Origin: "physical", DeviceEUI: m.ExpectedEUI, FirmwareID: m.FirmwareID, Checks: map[string]Status{
		"identity": NotRun, "storage_safety": NotRun, "storage_recovery": NotRun, "entropy_health": NotRun,
		"entropy_source_qualification": NotRun, "rf": NotRun, "thread": NotRun, "soak": NotRun,
		"rf_counters": NotRun, "rf_timing_fcs": NotRun, "thread_device": NotRun, "thread_counter_safety": NotRun,
	}}
	if m.Version != Version || m.RunID == "" || m.FirmwareID == "" {
		r.Reasons = append(r.Reasons, "invalid manifest")
		r.Checks["identity"] = Fail
		return r
	}
	last := map[string]uint64{}
	hello := map[string]bool{}
	acknowledged := false
	ackBoot := ""
	digests := map[string]bool{}
	threadBoot := ""
	threadSeen := false
	pendingWrite := false
	pendingBoot := ""
	for _, e := range events {
		r.Events++
		if e.Origin == "synthetic" {
			r.Origin = "synthetic"
			continue
		}
		if e.RunID != m.RunID || e.FirmwareID != m.FirmwareID || e.DeviceEUI != m.ExpectedEUI ||
			m.Board == "" || m.ChipRevision == "" || m.FlashID == "" || e.Version != Version {
			r.Checks["identity"] = Fail
			r.Reasons = append(r.Reasons, "evidence does not match fully identified approved board/firmware")
			continue
		}
		if e.Sequence <= last[e.BootID] {
			r.Checks["identity"] = Fail
			r.Reasons = append(r.Reasons, "duplicate/reordered event")
			continue
		}
		last[e.BootID] = e.Sequence
		if e.Stage == "hello" && e.Status == Pass {
			hello[e.BootID] = true
			if r.Checks["identity"] != Fail {
				r.Checks["identity"] = Pass
			}
			continue
		}
		if !hello[e.BootID] {
			r.Checks["identity"] = Fail
			r.Reasons = append(r.Reasons, "boot missing identity handshake")
			continue
		}
		if e.Probe == "storage" {
			if e.Stage == "prepare_write" {
				pendingWrite = true
				pendingBoot = e.BootID
			}
			if e.Stage == "observed" && e.Status == Pass && acknowledged && e.BootID != ackBoot {
				if e.Generation < r.LastAcknowledgedGeneration || e.Counter < r.LastAcknowledgedCounter {
					r.Checks["storage_safety"] = Fail
					r.Reasons = append(r.Reasons, "acknowledged state rolled back")
				} else if r.Checks["storage_safety"] != Fail {
					r.Checks["storage_safety"] = Pass
					if r.Checks["storage_recovery"] != Fail {
						r.Checks["storage_recovery"] = Pass
					}
					if e.BootID != pendingBoot {
						pendingWrite = false
					}
				}
			}
			if e.Stage == "ack" && e.Status == Pass {
				if e.BootID == pendingBoot {
					pendingWrite = false
				}
				acknowledged = true
				ackBoot = e.BootID
				if e.Generation < r.LastAcknowledgedGeneration || e.Counter < r.LastAcknowledgedCounter {
					r.Checks["storage_safety"] = Fail
				}
				r.LastAcknowledgedGeneration = max(r.LastAcknowledgedGeneration, e.Generation)
				r.LastAcknowledgedCounter = max(r.LastAcknowledgedCounter, e.Counter)
			}
			if e.Status == Fail {
				r.Checks["storage_recovery"] = Fail
				r.Reasons = append(r.Reasons, "storage failed closed or operation failed: "+e.Reason)
			}
		}
		if e.Probe == "entropy" && e.Stage == "health" {
			if e.Status == Pass && (len(e.Digest) != 64 || digests[e.Digest]) {
				r.Checks["entropy_health"] = Fail
				r.Reasons = append(r.Reasons, "missing/repeated entropy smoke digest")
			} else if r.Checks["entropy_health"] != Fail {
				r.Checks["entropy_health"] = e.Status
			}
			digests[e.Digest] = true
		}
		if e.Probe == "rf" && e.Stage == "summary" {
			x := e.Metrics
			valid := x["accepted"] >= 1000 && x["started"] == x["accepted"] && x["terminal"] == x["accepted"] &&
				x["success"]+x["noack"]+x["cca"]+x["abort"] == x["accepted"] &&
				x["rx_unique"] >= 1000 && x["rx_overflow"] == 0 &&
				x["channel"] >= 11 && x["channel"] <= 26 && (r.RFChannel == 0 || r.RFChannel == x["channel"])
			if valid && e.Status == Pass && r.Checks["rf_counters"] != Fail {
				r.Checks["rf_counters"] = Pass
				r.RFChannel = x["channel"]
			} else {
				r.Checks["rf_counters"], r.Checks["rf"] = Fail, Fail
			}
		}
		if e.Probe == "thread" && e.Stage == "summary" {
			x := e.Metrics
			if e.Status == Pass && x["role_child"] == 1 && x["secured_tx"] > 0 && x["echo_in"] > 0 && x["echo_out"] > 0 && r.Checks["thread_device"] != Fail {
				r.Checks["thread_device"] = Pass
			} else {
				r.Checks["thread_device"], r.Checks["thread"] = Fail, Fail
			}
		}
		if e.Probe == "thread" && e.Stage == "observation" && e.Status == Pass {
			x := e.Metrics
			if threadSeen && e.BootID != threadBoot {
				if x["mac_counter"] < r.LastMACCounter || x["mle_counter"] < r.LastMLECounter {
					r.Checks["thread_counter_safety"] = Fail
				} else if r.Checks["thread_counter_safety"] != Fail {
					r.Checks["thread_counter_safety"] = Pass
				}
			}
			threadSeen = true
			threadBoot = e.BootID
			r.LastMACCounter = max(r.LastMACCounter, x["mac_counter"])
			r.LastMLECounter = max(r.LastMLECounter, x["mle_counter"])
		}
		if e.Probe == "thread" && e.Stage == "soak" {
			if e.Status == Pass && e.Metrics["duration_seconds"] >= 1800 && e.Metrics["bounded_memory"] == 1 && r.Checks["soak"] != Fail {
				r.Checks["soak"] = Pass
			} else {
				r.Checks["soak"] = Fail
			}
		}
	}
	if r.Origin == "synthetic" {
		for k := range r.Checks {
			r.Checks[k] = NotRun
		}
		r.Reasons = append(r.Reasons, "synthetic fixtures exercise parsing only; physical evidence NOT_RUN")
	}
	if len(events) == 0 {
		r.Reasons = append(r.Reasons, "no device evidence; all physical tests NOT_RUN")
	}
	if pendingWrite && r.Checks["storage_recovery"] != Fail {
		r.Checks["storage_recovery"] = NotRun
		r.Reasons = append(r.Reasons, "unfinished write: completion/reboot observation missing, possible timeout")
	}
	// No statistical or software condition promotes this qualification. It
	// needs the source-backed analogue/clock/board review described in the lab
	// procedure, not an unsafe public entropy-verified boolean.
	r.Checks["entropy_source_qualification"] = NotRun
	return r
}

func Encode(e Event) ([]byte, error) {
	p, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	p = append([]byte(Prefix), p...)
	if _, err := Parse(p); err != nil {
		return nil, fmt.Errorf("invalid evidence: %w", err)
	}
	return append(p, '\n'), nil
}
