//go:build !tinygo && c6_owned_entry && c6_owned_hostfixture

package owned

/*
#include "fixture.h"
*/
import "C"

import "unsafe"

func FixtureNew() unsafe.Pointer                   { return C.c6o_fixture_new() }
func FixtureEvent(p unsafe.Pointer) unsafe.Pointer { return C.c6o_fixture_event(p) }
func FixtureDestroy(p unsafe.Pointer) uint32       { return uint32(C.c6o_fixture_destroy(p)) }
func FixtureRetire(p unsafe.Pointer) uint32        { return uint32(C.c6o_fixture_retire(p)) }
func FixtureForeignRetire(p unsafe.Pointer) uint32 { return uint32(C.c6o_fixture_foreign_retire(p)) }
func FixtureControl(p unsafe.Pointer) uint32       { return uint32(C.c6o_fixture_control(p)) }
func FixtureStatus(p unsafe.Pointer) int32         { return int32(C.c6o_fixture_status(p)) }
func FixtureReturns(p unsafe.Pointer) uint32       { return uint32(C.c6o_fixture_returns(p)) }
func FixtureForeignCall() uint32                   { return uint32(C.c6o_fixture_foreign_call()) }
func FixtureRaw(p unsafe.Pointer)                  { C.c6o_fixture_raw(p) }
