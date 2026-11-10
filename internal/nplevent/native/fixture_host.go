//go:build !tinygo && c6_event_hostfixture

package native

/*
#cgo CFLAGS: -DC6_EVENT_TEST_FIXTURE=1 -pthread
#cgo LDFLAGS: -pthread
#include "fixture_host.h"
*/
import "C"

func fixtureCallback() Callback { return Callback(C.c6_host_fixture_callback()) }
func fixtureReset()             { C.c6_host_fixture_reset() }
func fixtureWaitEntered()       { C.c6_host_fixture_wait_entered() }
func fixtureResume()            { C.c6_host_fixture_resume() }
func fixtureReturns() uint32    { return uint32(C.c6_host_fixture_returns()) }

type fixtureHandle struct{ value C.struct_c6_event_handle }

func fixtureSnapshot(owner *Event) fixtureHandle { return fixtureHandle{owner.handle} }

func fixtureClaim(handle fixtureHandle) error {
	key := lock()
	defer unlock(key)
	return bridgeError(C.c6_event_claim(&handle.value))
}

func fixtureFinish(handle fixtureHandle) (int32, error) {
	var status C.int32_t
	key := lock()
	defer unlock(key)
	err := bridgeError(C.c6_event_finish(&handle.value, &status))
	return int32(status), err
}

func fixtureInvoke(handle fixtureHandle) error {
	return bridgeError(C.c6_event_invoke(&handle.value))
}

func fixtureRelease(handle fixtureHandle) error {
	key := lock()
	defer unlock(key)
	return bridgeError(C.c6_event_release(&handle.value))
}

func fixtureNearWrap(handle fixtureHandle) error {
	key := lock()
	defer unlock(key)
	return bridgeError(C.c6_event_fixture_near_wrap(&handle.value))
}

func fixtureGeneration(handle fixtureHandle) uint32 { return uint32(handle.value.generation) }
func fixtureIndex(handle fixtureHandle) uint32      { return uint32(handle.value.index) }
