#include "example.h"
#include "../../internal/nplevent/owned/owned.h"
#include "../../internal/nplevent/publicabi/adapter.h"

volatile uint32_t c6_owned_compile_witness[16];
const volatile char c6_owned_compile_only_marker[] =
    "COMPILE ONLY - RUNTIME OWNED C GO YIELD - NO EXECUTION";
static struct ble_npl_event event;
static uint32_t argument = 0xc6017ead;

__attribute__((noinline))
static void native_callback(struct ble_npl_event *ev) {
    void *arg = ble_npl_event_get_arg(ev);
    if (arg != &argument || c6_owned_compile_only_marker[0] != 'C') {
        c6_owned_compile_witness[14] = 1; return;
    }
    int32_t application = 0;
    uint32_t control = c6_owned_call_go(1, &application);
    c6_owned_compile_witness[10] = control;
    c6_owned_compile_witness[11] = (uint32_t)application;
    c6_owned_compile_witness[15]++;
}
void *c6o_example_init(void) {
    ble_npl_event_init(&event, native_callback, &argument);
    return &event;
}
uint32_t c6o_example_retire(void) { return c6_public_deinit(&event); }
uint32_t c6o_example_foreign_retire(void) { return c6_public_deinit(&event); }
uint32_t c6o_example_check(void) {
    return c6_owned_compile_witness[14] || c6_owned_compile_witness[15] != 1 ||
        c6_owned_compile_witness[10] != 0 ||
        c6_owned_compile_witness[11] != UINT32_C(0x80000000) || event.event != NULL;
}
void c6o_example_record(uint32_t index, uint32_t value) {
    if (index < 10) c6_owned_compile_witness[index] = value;
}
