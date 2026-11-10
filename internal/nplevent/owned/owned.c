//go:build c6_owned_entry

#include "internal.h"
#include <string.h>

struct owner {
    void *identity;
    uintptr_t low, high, payload;
    struct c6_public_frame *frames;
    uint32_t live, go_calls;
};

#ifdef __riscv
/* These are leaf C ABI exports of the pinned, optional C6 runtime source.
 * They allocate/schedule nothing and do not manufacture a Go task. */
extern void *c6_tinygo_owned_identity(void);
extern uintptr_t c6_tinygo_owned_stack_low(void);
extern uint32_t c6_tinygo_owned_trap_depth(void);
static struct owner owners[8];
static uintptr_t enter(void) {
    uintptr_t prior;
    __asm__ volatile ("csrrci %0, mstatus, 8" : "=r"(prior) :: "memory");
    return prior;
}
static void leave(uintptr_t prior) {
    prior &= 8;
    __asm__ volatile ("csrs mstatus, %0" :: "r"(prior) : "memory");
}
static uintptr_t stack_pointer(void) {
    uintptr_t sp;
    __asm__ volatile ("mv %0, sp" : "=r"(sp));
    return sp;
}
uint32_t c6_owned_trap_active(void) {
    uintptr_t cause;
    __asm__ volatile ("csrr %0, mcause" : "=r"(cause));
    return cause != 0 || c6_tinygo_owned_trap_depth() != 0;
}
static struct owner *current(void) {
    if (c6_owned_trap_active()) return NULL;
    void *identity = c6_tinygo_owned_identity();
    if (!identity) return NULL;
    uintptr_t sp = stack_pointer();
    for (unsigned i = 0; i < 8; i++)
        if (owners[i].live && owners[i].identity == identity &&
            sp >= owners[i].low && sp <= owners[i].high)
            return &owners[i];
    return NULL;
}
uint32_t c6_owned_admission(void) {
    if (c6_owned_trap_active()) return C6_OWNED_TRAP;
    if (!c6_tinygo_owned_identity()) return C6_OWNED_UNOWNED;
    uintptr_t state;
    __asm__ volatile ("csrr %0, mstatus" : "=r"(state));
    return state & 8 ? C6_OWNED_OK : C6_OWNED_MASKED;
}
#else
#include <pthread.h>
static _Thread_local struct owner *active;
uint32_t c6_owned_trap_active(void) { return 0; }
uint32_t c6_owned_admission(void) { return C6_OWNED_OK; }
static struct owner *current(void) {
    /* A real locked Go/native thread scope, not a fixture goroutine number. */
    return active && active->live &&
        active->identity == (void *)pthread_self() ? active : NULL;
}
#endif

void *c6_owned_identity(void) {
    struct owner *o = current();
    return o ? o->identity : NULL;
}
uintptr_t c6_owned_payload(void) {
    struct owner *o = current();
    return o ? o->payload : 0;
}
struct c6_public_frame **c6_owned_frames(void) {
    struct owner *o = current();
    return o ? &o->frames : NULL;
}
static uint32_t call_go(struct owner *o, uint32_t request, int32_t *out) {
    if (!o || !out) return C6_OWNED_UNOWNED;
    uint32_t rc = c6_owned_admission();
    if (rc) return rc;
    o->go_calls++;
    rc = c6_owned_go_export(request, out);
    o->go_calls--;
    return rc;
}
uint32_t c6_owned_call_go(uint32_t request, int32_t *out) {
    if (!request || !out) return C6_OWNED_INVALID;
    /* No Go transition occurs on refusal. Identity and a live native scope
     * are obtained internally, never from caller integers or readiness flags. */
    if (c6_owned_trap_active()) return C6_OWNED_TRAP;
    return call_go(current(), request, out);
}
uint32_t c6_owned_scope_run(uintptr_t payload) {
    uint32_t rc = c6_owned_admission();
    if (rc) return rc;
#ifdef __riscv
    uintptr_t key = enter();
    void *identity = c6_tinygo_owned_identity();
    struct owner *o = NULL;
    for (unsigned i = 0; i < 8; i++) {
        if (owners[i].live && owners[i].identity == identity) {
            leave(key); return C6_OWNED_BUSY;
        }
        if (!owners[i].live && !o) o = &owners[i];
    }
    if (!o) { leave(key); return C6_OWNED_EXHAUSTED; }
    memset(o, 0, sizeof(*o));
    o->identity = identity;
    o->low = c6_tinygo_owned_stack_low();
    o->high = stack_pointer();
    if (!o->low || o->high < o->low) {
        memset(o, 0, sizeof(*o));
        leave(key); return C6_OWNED_UNOWNED;
    }
    o->live = 1;
    (void)payload; /* HOST GC handle is never a C6 identity issuer. */
    leave(key);
#else
    if (active) return C6_OWNED_BUSY;
    struct owner storage = {0};
    struct owner *o = &storage;
    o->identity = (void *)pthread_self();
    o->payload = payload;
    o->live = 1;
    active = o;
#endif
    int32_t status = 0;
    rc = call_go(o, 0, &status);
    /* A real scoped call cannot return with native frames/exports outstanding.
     * Never pop their lifetime into a success-shaped cleanup fallback. */
    if (o->frames || o->go_calls) __builtin_trap();
#ifdef __riscv
    key = enter();
    memset(o, 0, sizeof(*o));
    leave(key);
#else
    o->live = 0;
    active = NULL;
#endif
    return rc;
}
