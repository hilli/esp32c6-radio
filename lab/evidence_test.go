package lab

import (
	"strings"
	"testing"
)

func fixture(origin string) Event {
	return Event{Version: 1, Origin: origin, RunID: "run", BootID: "boot1", FirmwareID: "image", DeviceEUI: "600102fffe030405", Probe: "storage", Stage: "hello", Status: Pass, Sequence: 1}
}
func manifest() Manifest {
	return Manifest{Version: 1, RunID: "run", FirmwareID: "image", ExpectedEUI: "600102fffe030405", Board: "synthetic board identity", ChipRevision: "synthetic", FlashID: "synthetic"}
}

func TestSyntheticCannotPassPhysicalEvidence(t *testing.T) {
	e := fixture("synthetic")
	p, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	e, err = Parse(p[:len(p)-1])
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(manifest(), []Event{e})
	for k, v := range r.Checks {
		if v != NotRun {
			t.Fatal(k, v)
		}
	}
}
func TestMissingWrongIdentityAndSecretFieldsFail(t *testing.T) {
	if r := Evaluate(manifest(), nil); r.Checks["identity"] != NotRun {
		t.Fatal(r)
	}
	e := fixture("physical")
	e.DeviceEUI = "0000000000000000"
	if r := Evaluate(manifest(), []Event{e}); r.Checks["identity"] != Fail {
		t.Fatal(r)
	}
	p, _ := Encode(fixture("synthetic"))
	secret := strings.Replace(string(p), `"version":1`, `"version":1,"network_key":"never log this"`, 1)
	if _, err := Parse([]byte(strings.TrimSpace(secret))); err == nil {
		t.Fatal("secret/unknown evidence field accepted")
	}
}
func TestIndependentAcknowledgementLedgerRejectsRollback(t *testing.T) {
	h := fixture("physical")
	ack := h
	ack.Stage = "ack"
	ack.Sequence = 2
	ack.Generation = 5
	ack.Counter = 100
	boot := h
	boot.BootID = "boot2"
	observed := boot
	observed.Stage = "observed"
	observed.Sequence = 2
	observed.Generation = 4
	observed.Counter = 99
	r := Evaluate(manifest(), []Event{h, ack, boot, observed})
	if r.Checks["storage_safety"] != Fail {
		t.Fatal(r)
	}
	observed.Generation = 5
	observed.Counter = 100
	r = Evaluate(manifest(), []Event{h, ack, boot, observed})
	if r.Checks["storage_safety"] != Pass || r.Checks["entropy_source_qualification"] != NotRun {
		t.Fatal(r)
	}
}

func TestFailureCannotBeOverwrittenByLaterPass(t *testing.T) {
	for _, probe := range []string{"rf", "thread"} {
		h := fixture("physical")
		h.Probe = probe
		bad := h
		bad.Stage = "summary"
		bad.Sequence = 2
		bad.Status = Fail
		good := bad
		good.Sequence = 3
		good.Status = Pass
		good.Metrics = map[string]uint64{"accepted": 1000, "started": 1000, "terminal": 1000, "success": 1000, "rx_unique": 1000, "channel": 15, "role_child": 1, "secured_tx": 1, "echo_in": 1, "echo_out": 1}
		r := Evaluate(manifest(), []Event{h, bad, good})
		if r.Checks[probe] != Fail {
			t.Fatal(probe, r)
		}
		if probe == "rf" && r.Checks["rf_counters"] != Fail {
			t.Fatal(r)
		}
		if probe == "thread" && r.Checks["thread_device"] != Fail {
			t.Fatal(r)
		}
	}
}
