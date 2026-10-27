//go:build !tinygo || !esp32c6 || !esp32c6_radio

package settings

import esp32c6radio "github.com/hilli/esp32c6-radio"

func OpenSharedV2(*esp32c6radio.Radio) (*SharedV2, error) { return nil, ErrNativeUnavailable }
func FormatEmptySharedV2(*esp32c6radio.Radio) error       { return ErrNativeUnavailable }
