//go:build !tinygo || !esp32c6 || !esp32c6_radio

package settings

import esp32c6radio "github.com/hilli/esp32c6-radio"

func Open(*esp32c6radio.Radio) (*Store, error) { return nil, ErrNativeUnavailable }
func FormatEmpty(*esp32c6radio.Radio) error    { return ErrNativeUnavailable }
