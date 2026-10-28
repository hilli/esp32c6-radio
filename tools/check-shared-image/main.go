// Command check-shared-image checks only the closed experimental v2 profile.
// It performs no device operation and produces no physical PASS.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hilli/esp32c6-radio/internal/image"
)

func main() {
	manifest := flag.String("manifest", "internal/phy/native/c6-shared-v2-lab1.profile.json", "exact reviewed profile manifest")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-shared-image [-manifest path] image.elf image.bin")
		os.Exit(1)
	}
	if err := image.CheckSharedV2Manifest(*manifest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r, err := image.CheckSharedV2(flag.Arg(0), flag.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("c6-shared-v2-lab1: image=%d static SRAM=%d heap address space=%d; physical NOT_RUN\n", r.FlashBytes, r.StaticRAM, r.HeapBytes)
}
