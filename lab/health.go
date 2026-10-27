package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"math/bits"
)

type Health struct {
	Samples        uint64 `json:"samples"`
	Ones           uint64 `json:"ones"`
	LongestByteRun uint64 `json:"longest_byte_run"`
	RepeatedWords  uint64 `json:"repeated_words"`
	Digest         string `json:"digest"`
}

// SmokeHealth is deliberately a smoke test, not a min-entropy estimate,
// certification, substitute source, whitening step or entropy-verified flag.
func SmokeHealth(p []byte) (Health, Status) {
	h := Health{Samples: uint64(len(p))}
	run := uint64(0)
	var previous byte
	var lastWord uint32
	for i, b := range p {
		h.Ones += uint64(bits.OnesCount8(b))
		if i == 0 || b != previous {
			run = 1
		} else {
			run++
		}
		previous = b
		h.LongestByteRun = max(h.LongestByteRun, run)
		if i%4 == 3 {
			word := uint32(p[i-3]) | uint32(p[i-2])<<8 | uint32(p[i-1])<<16 | uint32(p[i])<<24
			if i >= 7 && word == lastWord {
				h.RepeatedWords++
			}
			lastWord = word
		}
	}
	digest := sha256.Sum256(p)
	h.Digest = hex.EncodeToString(digest[:])
	if len(p) < 4096 || h.LongestByteRun > 16 || h.RepeatedWords > 1 || h.Ones*100 < 40*h.Samples*8 || h.Ones*100 > 60*h.Samples*8 {
		return h, Fail
	}
	return h, Pass
}
