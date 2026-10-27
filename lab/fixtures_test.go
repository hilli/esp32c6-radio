package lab

import (
	"bufio"
	"os"
	"testing"
)

func TestCapturedSyntheticFixtureStaysNotRun(t *testing.T) {
	f, err := os.Open("testdata/synthetic-storage.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var events []Event
	for s.Scan() {
		e, err := Parse(s.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	m := manifest()
	m.RunID = "synthetic-run"
	m.FirmwareID = "synthetic-firmware"
	r := Evaluate(m, events)
	if len(events) != 4 || r.Origin != "synthetic" {
		t.Fatal(r)
	}
	for k, v := range r.Checks {
		if v != NotRun {
			t.Fatal(k, v)
		}
	}
}
