package threadnode

import "testing"

func TestNoVolatileCredentialsFallback(t *testing.T) {
	if err := Run(nil, nil, nil); err != ErrDurableSettingsRequired {
		t.Fatal("attempted hardware construction or credential use without durable storage", err)
	}
}
