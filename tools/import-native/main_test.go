package main

import (
	"strings"
	"testing"
)

func TestROMBindings(t *testing.T) {
	output, err := romAssembly([]byte("/* ignored */\nphy_param_rom = 0x4087fce8;\nphy_get_romfuncs = 0x40001108;\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"//go:build tinygo && esp32c6",
		`.weak phy_param_rom\n.set phy_param_rom, 0x4087fce8\n`,
		`.weak phy_get_romfuncs\n.set phy_get_romfuncs, 0x40001108\n`,
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}

func TestROMBindingsRejectEmpty(t *testing.T) {
	if _, err := romAssembly([]byte("not a ROM linker script")); err == nil {
		t.Fatal("accepted an empty ROM interface")
	}
}
