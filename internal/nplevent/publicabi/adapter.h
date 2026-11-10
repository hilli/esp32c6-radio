#ifndef C6_PUBLIC_EVENT_ADAPTER_H
#define C6_PUBLIC_EVENT_ADAPTER_H

#include "nimble_npl.h"

/* Native wrapper and arg pointees belong to the CALLER. Callback code must be
 * static C, never Go. Checked control results are NPL errors, NOT callback
 * status: the callback actually returns void. Public infallible signatures
 * fail-stop on misuse; checked forms allow callers to observe rejection.
 */
struct c6_public_ref {
    struct ble_npl_event *event;
    uint32_t cookie;
};

ble_npl_error_t c6_public_init(struct ble_npl_event *, ble_npl_event_fn *, void *);
ble_npl_error_t c6_public_deinit(struct ble_npl_event *);
ble_npl_error_t c6_public_get_arg(struct ble_npl_event *, void **);
ble_npl_error_t c6_public_set_arg(struct ble_npl_event *, void *);
ble_npl_error_t c6_public_is_queued(struct ble_npl_event *, bool *);
ble_npl_error_t c6_public_run(struct ble_npl_event *);

/* Private generation-bearing queue references; no caller pointer is
 * dereferenced after native retirement. No native eventq API is implemented. */
ble_npl_error_t c6_public_bind(struct ble_npl_event *, struct c6_public_ref *);
ble_npl_error_t c6_public_unbind(const struct c6_public_ref *);
ble_npl_error_t c6_public_reserve(const struct c6_public_ref *, uint32_t *added);
ble_npl_error_t c6_public_cancel(const struct c6_public_ref *);
ble_npl_error_t c6_public_claim(const struct c6_public_ref *);
ble_npl_error_t c6_public_invoke(const struct c6_public_ref *);
void c6_public_abi_read(uint32_t out[8]);
extern const volatile uint32_t c6_public_event_abi[8];
extern volatile uint32_t c6_public_first_fault;

struct c6_public_frame;
struct c6_public_frame **c6_public_frame_root(void);
struct c6_public_frame **c6_public_default_frames(void);
uint32_t c6_public_extra_trap_active(void);

#endif
