package lab

import "testing"

func TestIndependentWitnessCannotBeReplacedByCounterFlag(t *testing.T) {
	m := manifest()
	h := fixture("physical")
	h.Probe = "rf"
	e := h
	e.Stage = "summary"
	e.Sequence = 2
	e.Metrics = map[string]uint64{
		"accepted": 1000, "started": 1000, "terminal": 1000, "success": 1000, "rx_unique": 1000, "sniffer_ack_timing_artifact": 1, "channel": 15,
	}
	r := Evaluate(m, []Event{h, e})
	if r.Checks["rf_counters"] != Pass || r.Checks["rf"] != NotRun {
		t.Fatal("device metric certified external RF evidence", r)
	}
	w := Witness{Version: 1, Origin: "physical", RunID: m.RunID, DeviceEUI: m.ExpectedEUI, FirmwareID: m.FirmwareID, Kind: "sniffer", ArtifactSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Channel: 15, Frames: 1000, ValidFCS: 1000, MaxACKTurnaroundUS: 192}
	if err := ApplyWitness(m, &r, w, "wrong"); err == nil {
		t.Fatal("wrong capture passed")
	}
	if err := ApplyWitness(m, &r, w, w.ArtifactSHA256); err != nil || r.Checks["rf"] != Pass {
		t.Fatal(err, r)
	}
	w.Channel = 16
	if err := ApplyWitness(m, &r, w, w.ArtifactSHA256); err == nil {
		t.Fatal("unrelated channel witness accepted")
	}
	w.Channel = 15
	if err := ApplyWitness(m, &r, w, w.ArtifactSHA256); err == nil {
		t.Fatal("prior failure overwritten")
	}
}
func TestMissingBootAndSequenceCannotPass(t *testing.T) {
	m := manifest()
	e := fixture("physical")
	e.Stage = "observed"
	e.Generation = 9
	e.Counter = 100
	if r := Evaluate(m, []Event{e}); r.Checks["identity"] != Fail {
		t.Fatal(r)
	}
	h := fixture("physical")
	if r := Evaluate(m, []Event{h, h}); r.Checks["identity"] != Fail {
		t.Fatal(r)
	}
	if r := Evaluate(m, []Event{h}); r.Checks["storage_safety"] != NotRun {
		t.Fatal("no acknowledgement/reboot became storage pass", r)
	}
}
