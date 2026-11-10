//go:build !tinygo

package native

import "sync"

var critical sync.Mutex

func lock() uintptr  { critical.Lock(); return 0 }
func unlock(uintptr) { critical.Unlock() }
func inISR() bool    { return false }
