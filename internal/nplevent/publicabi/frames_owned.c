//go:build c6_owned_entry

#include "adapter.h"
#include "../owned/owned.h"

struct c6_public_frame **c6_public_frame_root(void) {
    struct c6_public_frame **owned = c6_owned_frames();
    return owned ? owned : c6_public_default_frames();
}
uint32_t c6_public_extra_trap_active(void) { return c6_owned_trap_active(); }
