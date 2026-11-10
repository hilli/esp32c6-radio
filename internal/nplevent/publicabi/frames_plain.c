//go:build !c6_owned_entry

#include "adapter.h"

struct c6_public_frame **c6_public_frame_root(void) { return c6_public_default_frames(); }
uint32_t c6_public_extra_trap_active(void) { return 0; }
