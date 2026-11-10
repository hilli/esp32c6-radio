#include "owned.h"

/* Go adapter implementation only; native clients use c6_owned_call_go.
 * The native gateway admits the real owner BEFORE this managed Go export. */
uint32_t c6_owned_scope_run(uintptr_t host_gc_handle);
extern uint32_t c6_owned_go_export(uint32_t request, int32_t *application_status);
