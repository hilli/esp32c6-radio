// Command check-image verifies a C6 ELF/bin pair, including physical flash
// offsets and conservative SRAM bounds. It does not execute the image.
package main

import (
	"fmt"
	"os"

	"github.com/hilli/esp32c6-radio/internal/image"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: check-image firmware.elf firmware.bin")
		os.Exit(2)
	}
	report, err := image.Check(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ESP32-C6: image=%d bytes, SRAM incl. IRAM/stack=%d bytes, heap=%d bytes\n",
		report.FlashBytes, report.StaticRAM, report.HeapBytes)
}
