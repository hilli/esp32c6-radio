//go:build !tinygo

package publicabi

func inISR() bool { return false }
