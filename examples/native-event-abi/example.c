#include "example.h"
#include "../../internal/nplevent/native/bridge.h"

volatile uint32_t c6_event_compile_witness[16];
const volatile char c6_event_compile_only_marker[] =
    "COMPILE ONLY - NOT FOR FLASH OR EXECUTION";

struct example_argument {
    uint32_t cookie;
    int32_t status;
};

__attribute__((noinline))
static int32_t c6_example_callback(void *bytes, uint32_t length) {
    if (c6_event_compile_only_marker[0] != 'C')
        return -1003;
    if (length != sizeof(struct example_argument) || (uintptr_t)bytes % 8)
        return -1001;
    struct example_argument *argument = bytes;
    if (argument->cookie != UINT32_C(0x6436c0de))
        return -1002;
    int32_t status = argument->status;
    argument->status = 0;
    c6_event_compile_witness[15]++;
    return status;
}

uintptr_t c6_example_callback_address(void) {
    c6_event_callback checked = c6_example_callback;
    return (uintptr_t)checked;
}

void c6_example_record(uint32_t index, uint32_t value) {
    if (index < 15)
        c6_event_compile_witness[index] = value;
}
