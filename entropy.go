// SPDX-FileCopyrightText: 2015-2025 Espressif Systems (Shanghai) CO LTD
// SPDX-License-Identifier: Apache-2.0
package esp32c6radio

import "errors"

var ErrEntropyUnavailable = errors.New("esp32c6-radio: PHY entropy source is unavailable")

// sampleEntropy mirrors ESP-IDF v5.5 hw_random.c's C6 path: 160*16 APB
// cycles at a 160:40 MHz CPU/APB ratio, four byte-wise asynchronous LP timer
// mixes, and fresh RNG register samples. It is not a PRNG substitute.
func sampleEntropy(last *uint32, cycles, random, timer func() uint32) (uint32, error) {
	var result, cycle uint32
	for i := uint(0); i < 4; i++ {
		for attempts := 0; ; attempts++ {
			cycle = cycles()
			result ^= random()
			if cycle-*last >= 4*160*16 {
				break
			}
			if attempts == 65535 {
				return 0, ErrEntropyUnavailable
			}
		}
		counter := timer() & 0xff
		result ^= ((result ^ counter) & 0xff) << (i * 8)
	}
	*last = cycle
	return result ^ random(), nil
}
