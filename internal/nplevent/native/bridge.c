#include "bridge.h"
#include <string.h>

struct c6_event_slot {
    c6_event_callback callback;
    uint32_t generation, length, in_use, running, returned;
    int32_t status, first_failure;
    uint32_t retired;
    _Alignas(8) unsigned char argument[C6_EVENT_ARGUMENT_BYTES];
};

static struct c6_event_slot c6_event_slots[C6_EVENT_SLOTS];

_Static_assert(sizeof(struct c6_event_handle) == 8, "private handle ABI");
_Static_assert(offsetof(struct c6_event_handle, generation) == 4, "generation ABI");
_Static_assert(sizeof(c6_event_callback) == sizeof(uintptr_t), "callback address ABI");
_Static_assert(offsetof(struct c6_event_slot, argument) % 8 == 0, "argument alignment");

/* Volatile reads preserve the actual target descriptor in the compile-only ELF. */
const volatile uint32_t c6_event_bridge_abi[8] = {
    1, sizeof(struct c6_event_handle), _Alignof(struct c6_event_handle),
    sizeof(c6_event_callback), C6_EVENT_ARGUMENT_BYTES, 8,
    sizeof(struct c6_event_slot), C6_EVENT_SLOTS
};

static struct c6_event_slot *lookup(const struct c6_event_handle *handle) {
    if (!handle || handle->index == 0 || handle->index > C6_EVENT_SLOTS)
        return NULL;
    struct c6_event_slot *slot = &c6_event_slots[handle->index - 1];
    if (!slot->in_use || slot->generation != handle->generation)
        return NULL;
    return slot;
}

int32_t c6_event_create(uintptr_t callback, const void *argument, uint32_t length,
                        struct c6_event_handle *out) {
    if (!callback || !out || length > C6_EVENT_ARGUMENT_BYTES || (length && !argument))
        return C6_EVENT_INVALID;
    for (uint32_t i = 0; i < C6_EVENT_SLOTS; i++) {
        struct c6_event_slot *slot = &c6_event_slots[i];
        if (slot->in_use || slot->retired)
            continue;
        if (slot->generation == UINT32_MAX) {
            slot->retired = 1;
            continue;
        }
        slot->generation++;
        slot->callback = (c6_event_callback)callback;
        slot->length = length;
        slot->in_use = 1;
        slot->running = slot->returned = 0;
        slot->status = slot->first_failure = 0;
        memset(slot->argument, 0, sizeof(slot->argument));
        if (length)
            memcpy(slot->argument, argument, length);
        out->index = i + 1;
        out->generation = slot->generation;
        return C6_EVENT_OK;
    }
    return C6_EVENT_EXHAUSTED;
}

int32_t c6_event_claim(const struct c6_event_handle *handle) {
    struct c6_event_slot *slot = lookup(handle);
    if (!slot)
        return C6_EVENT_STALE;
    if (slot->running)
        return C6_EVENT_BUSY;
    slot->running = 1;
    slot->returned = 0;
    return C6_EVENT_OK;
}

__attribute__((noinline))
int32_t c6_event_invoke(const struct c6_event_handle *handle) {
    struct c6_event_slot *slot = lookup(handle);
    if (!slot)
        return C6_EVENT_STALE;
    if (!slot->running || slot->returned)
        return C6_EVENT_INVALID;
    slot->status = slot->callback(slot->argument, slot->length);
    slot->returned = 1;
    return C6_EVENT_OK;
}

int32_t c6_event_finish(const struct c6_event_handle *handle, int32_t *status) {
    struct c6_event_slot *slot = lookup(handle);
    if (!slot)
        return C6_EVENT_STALE;
    if (!status || !slot->running)
        return C6_EVENT_INVALID;
    if (!slot->returned)
        return C6_EVENT_UNRETURNED;
    *status = slot->status;
    if (slot->status != 0 && slot->first_failure == 0)
        slot->first_failure = slot->status;
    slot->running = 0;
    return C6_EVENT_OK;
}

int32_t c6_event_release(const struct c6_event_handle *handle) {
    struct c6_event_slot *slot = lookup(handle);
    if (!slot)
        return C6_EVENT_STALE;
    if (slot->running)
        return C6_EVENT_BUSY;
    slot->callback = NULL;
    slot->in_use = 0;
    memset(slot->argument, 0, sizeof(slot->argument));
    return C6_EVENT_OK;
}

int32_t c6_event_first_failure(const struct c6_event_handle *handle, int32_t *status) {
    struct c6_event_slot *slot = lookup(handle);
    if (!slot)
        return C6_EVENT_STALE;
    if (!status)
        return C6_EVENT_INVALID;
    if (slot->running)
        return C6_EVENT_BUSY;
    *status = slot->first_failure;
    return C6_EVENT_OK;
}

void c6_event_abi_read(uint32_t out[8]) {
    for (unsigned i = 0; i < 8; i++)
        out[i] = c6_event_bridge_abi[i];
}

#ifdef C6_EVENT_TEST_FIXTURE
/* HOST fixture only: reach the real UINT32_MAX retirement arm without billions
 * of allocations. Never compiled into the C6 bridge. */
int32_t c6_event_fixture_near_wrap(const struct c6_event_handle *released) {
    if (!released || released->index == 0 || released->index > C6_EVENT_SLOTS)
        return C6_EVENT_INVALID;
    struct c6_event_slot *slot = &c6_event_slots[released->index - 1];
    if (slot->in_use || slot->generation != released->generation || slot->retired)
        return C6_EVENT_BUSY;
    slot->generation = UINT32_MAX - 1;
    return C6_EVENT_OK;
}
#endif
