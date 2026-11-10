#include "adapter.h"

#ifdef __riscv
static struct c6_public_frame *root;
#else
static _Thread_local struct c6_public_frame *root;
#endif

struct c6_public_frame **c6_public_default_frames(void) { return &root; }
