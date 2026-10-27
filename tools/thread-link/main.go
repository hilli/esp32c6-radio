//go:build tinygo && esp32c6 && esp32c6_radio

// Link-only validation harness. NEVER FLASH: it has no durable settings.
package main

import (
	"errors"

	threadnode "github.com/hilli/esp32c6-radio/examples/thread-node"
	"github.com/hilli/gothread/platform"
)

type unavailableSettings struct{}

func (unavailableSettings) Get(platform.SettingsKey, int) ([]byte, error) {
	return nil, errors.New("link-only storage is unavailable")
}
func (unavailableSettings) Set(platform.SettingsKey, []byte) error {
	return errors.New("link-only storage is unavailable")
}
func (unavailableSettings) Add(platform.SettingsKey, []byte) error {
	return errors.New("link-only storage is unavailable")
}
func (unavailableSettings) Delete(platform.SettingsKey, int) error {
	return errors.New("link-only storage is unavailable")
}
func (unavailableSettings) Wipe() error { return errors.New("link-only storage is unavailable") }

func main() {
	println("LINK-ONLY: no operational Thread node or durable credentials")
	if err := threadnode.Run(unavailableSettings{}, nil, nil); err != nil {
		println(err.Error())
	}
	select {}
}
