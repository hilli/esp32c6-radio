#ifndef C6_PRIVATE_EVENT_BRIDGE_H
#define C6_PRIVATE_EVENT_BRIDGE_H

#include <stddef.h>
#include <stdint.h>

/* PRIVATE ABI, not ble_npl_event or the vendor's void(event *) callback.
 * The callback borrows bridge-owned, 8-byte-aligned argument bytes until return.
 * Code must be statically linked; Go pointers, retained argument aliases,
 * callback-to-Go entry, foreign task blocking and nonlocal unwind are unsupported.
 */
typedef int32_t (*c6_event_callback)(void *argument, uint32_t length);

struct c6_event_handle {
    uint32_t index;
    uint32_t generation;
};

enum {
    C6_EVENT_SLOTS = 8,
    C6_EVENT_ARGUMENT_BYTES = 32,
    C6_EVENT_OK = 0,
    C6_EVENT_INVALID = 1,
    C6_EVENT_STALE = 2,
    C6_EVENT_BUSY = 3,
    C6_EVENT_EXHAUSTED = 4,
    C6_EVENT_UNRETURNED = 5
};

/* Except invoke, these private calls require the Go bridge's saved-MIE/mutex
 * critical section. Invoke requires a successful claim and exclusive callback
 * ownership. No pointer to a handle or Go argument is retained by C.
 */
int32_t c6_event_create(uintptr_t callback, const void *argument, uint32_t length,
                        struct c6_event_handle *out);
int32_t c6_event_claim(const struct c6_event_handle *handle);
int32_t c6_event_invoke(const struct c6_event_handle *handle);
int32_t c6_event_finish(const struct c6_event_handle *handle, int32_t *status);
int32_t c6_event_release(const struct c6_event_handle *handle);
int32_t c6_event_first_failure(const struct c6_event_handle *handle, int32_t *status);
void c6_event_abi_read(uint32_t out[8]);
extern const volatile uint32_t c6_event_bridge_abi[8];

#ifdef C6_EVENT_TEST_FIXTURE
int32_t c6_event_fixture_near_wrap(const struct c6_event_handle *released);
#endif

#endif
