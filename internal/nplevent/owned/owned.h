#ifndef C6_GO_OWNED_ENTRY_H
#define C6_GO_OWNED_ENTRY_H
#include <stdint.h>
#include <stddef.h>

enum {
    C6_OWNED_OK = 0, C6_OWNED_UNOWNED = 1, C6_OWNED_TRAP = 2,
    C6_OWNED_MASKED = 3, C6_OWNED_EXHAUSTED = 4, C6_OWNED_BUSY = 5,
    C6_OWNED_PANIC = 6, C6_OWNED_INVALID = 7
};
struct c6_public_frame;
uint32_t c6_owned_call_go(uint32_t request, int32_t *application_status);
void *c6_owned_identity(void);
uintptr_t c6_owned_payload(void);
struct c6_public_frame **c6_owned_frames(void);
uint32_t c6_owned_trap_active(void);
uint32_t c6_owned_admission(void);

#endif
