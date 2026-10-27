package lab

import "testing"

func TestMissingCompletionCannotReuseEarlierPass(t *testing.T) {
	h := fixture("physical")
	ack := h
	ack.Stage = "ack"
	ack.Sequence = 2
	ack.Generation = 5
	ack.Counter = 100
	b := h
	b.BootID = "boot2"
	observed := b
	observed.Stage = "observed"
	observed.Sequence = 2
	observed.Generation = 5
	observed.Counter = 100
	pending := b
	pending.Stage = "prepare_write"
	pending.Status = NotRun
	pending.Sequence = 3
	r := Evaluate(manifest(), []Event{h, ack, b, observed, pending})
	if r.Checks["storage_recovery"] != NotRun {
		t.Fatal("missing completion reported prior success", r)
	}
}
