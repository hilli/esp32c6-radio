package lab

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestHealthSmokeRejectsStuckAndShortSources(t *testing.T) {
	for _, p := range [][]byte{make([]byte, 4096), bytes.Repeat([]byte{0xaa}, 4096), make([]byte, 1)} {
		if _, status := SmokeHealth(p); status != Fail {
			t.Fatal("stuck/short source passed")
		}
	}
	// Explicitly synthetic PRNG fixture. Passing this MUST NOT qualify entropy.
	p := make([]byte, 8192)
	r := rand.New(rand.NewSource(42))
	_, _ = r.Read(p)
	h, status := SmokeHealth(p)
	if status != Pass || h.Samples != 8192 || len(h.Digest) != 64 {
		t.Fatal(h, status)
	}
}
