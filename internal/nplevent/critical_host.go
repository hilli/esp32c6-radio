//go:build !tinygo

package nplevent

import (
	"sync"
	"unsafe"
)

var critical sync.Mutex

const pointerBytes = int(unsafe.Sizeof(uintptr(0)))

func lock() uintptr  { critical.Lock(); return 0 }
func unlock(uintptr) { critical.Unlock() }
func inISR() bool    { return false }
