package esp32c6radio

import "unsafe"

func dmaBytes(words *[33]uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(words)), 132)
}
