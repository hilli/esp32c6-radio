package esp32c6radio

import "testing"

func TestEntropySamplingAndWrap(t *testing.T) {
	for _, start := range []uint32{0, ^uint32(0) - 100} {
		last := start
		cycle := start
		reads, timers := 0, 0
		word, err := sampleEntropy(&last, func() uint32 { cycle += 100; return cycle },
			func() uint32 { reads++; return uint32(reads) * 0x1020304 },
			func() uint32 { timers++; return uint32(timers) })
		if err != nil || timers != 4 || reads < 107 || last-start < 10240 || word == 0 {
			t.Fatal("sampling contract", word, err, reads, timers, last-start)
		}
	}
}

func TestEntropyClockFailureIsExplicit(t *testing.T) {
	var last uint32
	_, err := sampleEntropy(&last, func() uint32 { return 0 }, func() uint32 { return 42 }, func() uint32 { return 1 })
	if err != ErrEntropyUnavailable || last != 0 {
		t.Fatal("stalled clock produced success-shaped entropy", err)
	}
}
