//go:build tinygo && esp32c6 && c6_owned_entry

package owned

/*
#include "internal.h"
extern void *c6_tinygo_owned_identity(void);
*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"
)

var roots = struct {
	sync.Mutex
	owners map[unsafe.Pointer]*scope
}{owners: make(map[unsafe.Pointer]*scope)}

func platformRun(s *scope) (uint32, error) {
	identity := C.c6_tinygo_owned_identity()
	if identity == nil {
		return uint32(ErrUnowned), nil
	}
	roots.Lock()
	if roots.owners[identity] != nil {
		roots.Unlock()
		return uint32(ErrBusy), nil
	}
	// The actual Go Task pointer is a strong Go root, not merely a C integer.
	roots.owners[identity] = s
	roots.Unlock()
	returned := false
	defer func() {
		if returned {
			roots.Lock()
			delete(roots.owners, identity)
			roots.Unlock()
		}
		runtime.KeepAlive(identity)
	}()
	rc := C.c6_owned_scope_run(0)
	returned = true
	return uint32(rc), nil
}

func platformCurrent() *scope {
	identity := C.c6_owned_identity()
	if identity == nil {
		return nil
	}
	roots.Lock()
	s := roots.owners[identity]
	roots.Unlock()
	return s
}
