//go:build !tinygo && c6_public_hostfixture

package publicabi

/*
#include "fixture.h"
*/
import "C"

import "unsafe"

func newFixture(mode uint32) (unsafe.Pointer, error) {
	var rc C.int32_t
	owner := C.c6p_fixture_new(C.uint32_t(mode), &rc)
	return owner, control(C.ble_npl_error_t(rc))
}
func fixtureEvent(p unsafe.Pointer) unsafe.Pointer { return unsafe.Pointer(C.c6p_fixture_event(p)) }
func destroyFixture(p unsafe.Pointer) error {
	return control(C.ble_npl_error_t(C.c6p_fixture_destroy(p)))
}
func fixtureCalls(p unsafe.Pointer) uint32 { return uint32(C.c6p_fixture_calls(p)) }
func fixtureIssue(p unsafe.Pointer) uint32 { return uint32(C.c6p_fixture_issue(p)) }
func fixtureLive() uint32                  { return uint32(C.c6p_fixture_live()) }
func fixtureWait()                         { C.c6p_fixture_wait() }
func fixtureResume()                       { C.c6p_fixture_resume() }
func fixtureTry(p unsafe.Pointer, op uint32) error {
	return control(C.ble_npl_error_t(C.c6p_fixture_try(p, C.uint32_t(op))))
}
func fixturePublicCycle(p unsafe.Pointer) { C.c6p_fixture_public_cycle(p) }
func fixtureQueued(p unsafe.Pointer) bool { return C.c6p_fixture_queued(p) != 0 }
func fixtureReinit(p unsafe.Pointer)      { C.c6p_fixture_reinit(p) }
func fixtureArmFaultExit()                { C.c6p_fixture_arm_fault_exit() }
