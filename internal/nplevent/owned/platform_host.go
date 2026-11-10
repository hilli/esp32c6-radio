//go:build !tinygo && c6_owned_entry

package owned

/*
#include "internal.h"
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
)

func platformRun(s *scope) (uint32, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	handle := cgo.NewHandle(s)
	returned := false
	defer func() {
		if returned {
			handle.Delete()
		}
		// Unsupported nonreturn/Goexit never authorizes freeing a native lease.
	}()
	rc := C.c6_owned_scope_run(C.uintptr_t(handle))
	returned = true
	return uint32(rc), nil
}

func platformCurrent() *scope {
	if C.c6_owned_identity() == nil {
		return nil
	}
	handle := C.c6_owned_payload()
	if handle == 0 {
		return nil
	}
	return cgo.Handle(handle).Value().(*scope)
}
