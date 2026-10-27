package image

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"
)

func testImage(hash bool) ([]byte, []segment) {
	segments := []segment{
		{0x40804000, []byte{1, 2, 3, 4}},
		{flashBase + 44, []byte{5, 6, 7, 8}},
	}
	bin := make([]byte, 24)
	bin[0], bin[1], bin[12] = 0xe9, 2, 13
	binary.LittleEndian.PutUint32(bin[4:], 0x40804000)
	if hash {
		bin[23] = 1
	}
	checksum := byte(0xef)
	for _, s := range segments {
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header, s.address)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(s.data)))
		bin = append(bin, header...)
		bin = append(bin, s.data...)
		for _, b := range s.data {
			checksum ^= b
		}
	}
	for len(bin)%16 != 15 {
		bin = append(bin, 0)
	}
	bin = append(bin, checksum)
	if hash {
		sum := sha256.Sum256(bin)
		bin = append(bin, sum[:]...)
	}
	return bin, segments
}

func TestROMImage(t *testing.T) {
	for _, hash := range []bool{false, true} {
		bin, segments := testImage(hash)
		if err := checkBinary(bin, 0x40804000, segments); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRejectInvalidImage(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]byte, []segment) ([]byte, []segment)
		want string
	}{
		{"wrong-chip", func(b []byte, s []segment) ([]byte, []segment) { b[12] = 5; return b, s }, "ESP32-C6"},
		{"entry", func(b []byte, s []segment) ([]byte, []segment) { b[4]++; return b, s }, "entry"},
		{"count", func(b []byte, s []segment) ([]byte, []segment) { b[1]++; return b, s }, "count"},
		{"truncated", func(b []byte, s []segment) ([]byte, []segment) { return b[:40], s }, "truncated"},
		{"payload", func(b []byte, s []segment) ([]byte, []segment) { b[32]++; return b, s }, "payload"},
		{"checksum", func(b []byte, s []segment) ([]byte, []segment) { b[len(b)-1]++; return b, s }, "checksum"},
		{"padding", func(b []byte, s []segment) ([]byte, []segment) { b[55] = 1; return b, s }, "padding"},
		{"trailer", func(b []byte, s []segment) ([]byte, []segment) { return append(b, 1), s }, "trailer"},
		{"nonidentity", func(b []byte, s []segment) ([]byte, []segment) {
			s[1].address += 0x800000
			binary.LittleEndian.PutUint32(b[36:], s[1].address)
			return b, s
		}, "nonidentity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, segments := testImage(false)
			bin, segments = tc.edit(bin, segments)
			err := checkBinary(bin, 0x40804000, segments)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	bin, segments := testImage(true)
	bin[len(bin)-1] ^= 1
	if err := checkBinary(bin, 0x40804000, segments); err == nil {
		t.Fatal("accepted a corrupted appended hash")
	}
}
