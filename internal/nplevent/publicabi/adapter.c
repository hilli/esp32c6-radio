#include "adapter.h"
#include <limits.h>
#include <string.h>

#ifdef __riscv
#if __riscv_xlen != 32
#error "This native event port only supports the selected RV32 C6 ABI"
#endif
static uintptr_t enter(void) {
    uintptr_t state;
    __asm__ volatile ("csrrci %0, mstatus, 8" : "=r"(state) :: "memory");
    return state;
}
static void leave(uintptr_t state) {
    state &= 8;
    __asm__ volatile ("csrs mstatus, %0" :: "r"(state) : "memory");
}
static bool in_isr(void) {
    uintptr_t cause;
    __asm__ volatile ("csrr %0, mcause" : "=r"(cause));
    return cause != 0 || c6_public_extra_trap_active();
}
#else
#include <pthread.h>
static pthread_mutex_t gate = PTHREAD_MUTEX_INITIALIZER;
static uintptr_t enter(void) { pthread_mutex_lock(&gate); return 0; }
static void leave(uintptr_t state) { (void)state; pthread_mutex_unlock(&gate); }
static bool in_isr(void) { return c6_public_extra_trap_active() != 0; }
#endif

#define SLOT_COUNT 8
#define MAX_GENERATION (UINT32_MAX >> 4)
struct slot {
    struct ble_npl_event *owner;
    ble_npl_event_fn *callback;
    void *argument;
    uint32_t generation, live, retiring, queued, running, bound, invoked;
};
static struct slot slots[SLOT_COUNT];

/* The explicit owned-entry build selects runtime-owner frame storage.
 * The ordinary build retains the original no-reentry/no-yield C-only root. */
struct c6_public_frame { struct slot *slot; struct c6_public_frame *parent; };
#define frames (*c6_public_frame_root())

static bool on_stack(struct slot *slot) {
    for (struct c6_public_frame *f = frames; f; f = f->parent)
        if (f->slot == slot)
            return true;
    return false;
}

_Static_assert(sizeof(struct ble_npl_event) == sizeof(void *), "public event wrapper");
_Static_assert(offsetof(struct ble_npl_event, event) == 0, "public wrapper field");
const volatile uint32_t c6_public_event_abi[8] = {
    1, sizeof(struct ble_npl_event), _Alignof(struct ble_npl_event),
    sizeof(ble_npl_event_fn *), sizeof(void *), sizeof(struct c6_public_ref),
    sizeof(struct slot), SLOT_COUNT
};
volatile uint32_t c6_public_first_fault;

static uint32_t cookie(struct slot *s) {
    return (s->generation << 4) | ((uint32_t)(s - slots) + 1);
}
static struct slot *by_ref(const struct c6_public_ref *ref) {
    if (!ref || !(ref->cookie & 15) || (ref->cookie & 15) > SLOT_COUNT)
        return NULL;
    struct slot *s = &slots[(ref->cookie & 15) - 1];
    if (!s->live || s->owner != ref->event || cookie(s) != ref->cookie)
        return NULL;
    return s;
}
static struct slot *by_event(struct ble_npl_event *ev) {
    if (!ev || (uintptr_t)ev->event > UINT32_MAX)
        return NULL;
    struct c6_public_ref ref = { ev, (uint32_t)(uintptr_t)ev->event };
    struct slot *s = by_ref(&ref);
    return s && !s->retiring ? s : NULL;
}
static void collect(struct slot *s) {
    if (s->retiring && !s->running && !s->queued && !s->bound) {
        uint32_t generation = s->generation;
        memset(s, 0, sizeof(*s));
        s->generation = generation;
    }
}
static void fault(ble_npl_error_t error) {
    uintptr_t key = enter();
    if (!c6_public_first_fault)
        c6_public_first_fault = error;
    leave(key);
    __builtin_trap();
}

ble_npl_error_t c6_public_init(struct ble_npl_event *ev, ble_npl_event_fn *fn, void *arg) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!ev || !fn) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    struct slot *old = by_event(ev);
    if (ev->event && !old) { leave(key); return BLE_NPL_ENOENT; }
    if (old && (old->running || old->queued)) { leave(key); return BLE_NPL_EBUSY; }
    struct slot *next = NULL;
    for (unsigned i = 0; i < SLOT_COUNT; i++)
        if (!slots[i].live && slots[i].generation < MAX_GENERATION) {
            next = &slots[i]; break;
        }
    if (!next && old && !old->bound && old->generation < MAX_GENERATION)
        next = old;
    if (!next) { leave(key); return BLE_NPL_ENOMEM; }
    if (old) { old->retiring = 1; collect(old); }
    uint32_t generation = next->generation + 1;
    memset(next, 0, sizeof(*next));
    next->owner = ev; next->callback = fn; next->argument = arg;
    next->generation = generation; next->live = 1;
    ev->event = (void *)(uintptr_t)cookie(next);
    leave(key);
    return BLE_NPL_OK;
}

ble_npl_error_t c6_public_deinit(struct ble_npl_event *ev) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!ev) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    if (!ev->event) { leave(key); return BLE_NPL_OK; }
    struct slot *s = by_event(ev);
    if (!s) { leave(key); return BLE_NPL_ENOENT; }
    if (s->queued || (s->running && !on_stack(s))) {
        leave(key); return BLE_NPL_EBUSY;
    }
    /* A real cached callback deinitializes and returns ev to its caller's
     * pool before callback return. Detach now; never touch ev after fn(ev). */
    ev->event = NULL;
    s->retiring = 1;
    collect(s);
    leave(key);
    return BLE_NPL_OK;
}

ble_npl_error_t c6_public_get_arg(struct ble_npl_event *ev, void **out) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!out) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    struct slot *s = by_event(ev);
    ble_npl_error_t rc = !s ? BLE_NPL_ENOENT :
        s->running && !on_stack(s) ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) *out = s->argument;
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_set_arg(struct ble_npl_event *ev, void *arg) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_event(ev);
    ble_npl_error_t rc = !s ? BLE_NPL_ENOENT :
        s->queued || (s->running && !on_stack(s)) ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) s->argument = arg;
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_is_queued(struct ble_npl_event *ev, bool *out) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!out) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    struct slot *s = by_event(ev);
    if (s) *out = s->queued != 0;
    leave(key);
    return s ? BLE_NPL_OK : BLE_NPL_ENOENT;
}

ble_npl_error_t c6_public_bind(struct ble_npl_event *ev, struct c6_public_ref *out) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!out) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    struct slot *s = by_event(ev);
    ble_npl_error_t rc = !s ? BLE_NPL_ENOENT :
        s->bound || s->running || s->queued ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) {
        out->event = ev; out->cookie = cookie(s); s->bound = 1;
    }
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_unbind(const struct c6_public_ref *ref) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_ref(ref);
    ble_npl_error_t rc = !s || !s->bound ? BLE_NPL_ENOENT :
        s->running || s->queued ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) { s->bound = 0; collect(s); }
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_reserve(const struct c6_public_ref *ref, uint32_t *added) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    if (!added) return BLE_NPL_EINVAL;
    uintptr_t key = enter();
    struct slot *s = by_ref(ref);
    ble_npl_error_t rc = !s || !s->bound || s->retiring ? BLE_NPL_ENOENT :
        s->running ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) { *added = !s->queued; s->queued = 1; }
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_cancel(const struct c6_public_ref *ref) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_ref(ref);
    ble_npl_error_t rc = !s || !s->bound ? BLE_NPL_ENOENT : BLE_NPL_OK;
    if (s && s->bound) { s->queued = 0; collect(s); }
    leave(key);
    return rc;
}
ble_npl_error_t c6_public_claim(const struct c6_public_ref *ref) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_ref(ref);
    ble_npl_error_t rc = !s || !s->bound ? BLE_NPL_ENOENT :
        s->running ? BLE_NPL_EBUSY : BLE_NPL_OK;
    if (rc == BLE_NPL_OK) {
        if (s->retiring) { s->queued = 0; rc = BLE_NPL_ENOENT; }
        else if (!s->queued) rc = BLE_NPL_EBUSY;
        else { s->queued = 0; s->running = 1; s->invoked = 0; }
    }
    leave(key);
    return rc;
}

__attribute__((noinline))
ble_npl_error_t c6_public_invoke(const struct c6_public_ref *ref) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_ref(ref);
    if (!s || !s->running || s->invoked) {
        leave(key); return s ? BLE_NPL_EBUSY : BLE_NPL_ENOENT;
    }
    s->invoked = 1;
    ble_npl_event_fn *fn = s->callback;
    struct ble_npl_event *ev = s->owner;
    struct c6_public_frame frame = {s, frames};
    frames = &frame;
    leave(key);
    fn(ev);
    key = enter();
    frames = frame.parent;
    /* Only retained port state, never the caller wrapper or arg, after fn. */
    s->running = s->invoked = 0;
    collect(s);
    leave(key);
    return BLE_NPL_OK;
}
ble_npl_error_t c6_public_run(struct ble_npl_event *ev) {
    if (in_isr()) return BLE_NPL_ERR_IN_ISR;
    uintptr_t key = enter();
    struct slot *s = by_event(ev);
    if (!s || s->running || s->queued) {
        leave(key); return s ? BLE_NPL_EBUSY : BLE_NPL_ENOENT;
    }
    struct c6_public_ref ref = { ev, cookie(s) };
    s->running = 1; s->invoked = 0;
    leave(key);
    return c6_public_invoke(&ref);
}

void ble_npl_event_init(struct ble_npl_event *ev, ble_npl_event_fn *fn, void *arg) {
    ble_npl_error_t rc = c6_public_init(ev, fn, arg); if (rc) fault(rc);
}
void ble_npl_event_deinit(struct ble_npl_event *ev) {
    ble_npl_error_t rc = c6_public_deinit(ev); if (rc) fault(rc);
}
void *ble_npl_event_get_arg(struct ble_npl_event *ev) {
    void *arg = NULL;
    ble_npl_error_t rc = c6_public_get_arg(ev, &arg); if (rc) fault(rc);
    return arg;
}
void ble_npl_event_set_arg(struct ble_npl_event *ev, void *arg) {
    ble_npl_error_t rc = c6_public_set_arg(ev, arg); if (rc) fault(rc);
}
bool ble_npl_event_is_queued(struct ble_npl_event *ev) {
    bool queued = false;
    ble_npl_error_t rc = c6_public_is_queued(ev, &queued); if (rc) fault(rc);
    return queued;
}
__attribute__((noinline))
void ble_npl_event_run(struct ble_npl_event *ev) {
    ble_npl_error_t rc = c6_public_run(ev); if (rc) fault(rc);
}
void c6_public_abi_read(uint32_t out[8]) {
    for (unsigned i = 0; i < 8; i++) out[i] = c6_public_event_abi[i];
}
