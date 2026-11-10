#include "example.h"
#include "../../internal/nplevent/publicabi/adapter.h"

volatile uint32_t c6_public_compile_witness[16];
const volatile char c6_public_compile_only_marker[] =
    "COMPILE ONLY - PUBLIC EVENT ABI - NO FLASH OR EXECUTION";
static struct ble_npl_event event;
struct argument { uint32_t identity, generation; };
static struct argument first = {0xc6e0a192, 1}, second = {0xc6e0a192, 2};

__attribute__((noinline))
static void public_callback(struct ble_npl_event *ev) {
    struct argument *arg = ble_npl_event_get_arg(ev);
    if (c6_public_compile_only_marker[0] != 'C' || ev != &event ||
        !arg || arg->identity != UINT32_C(0xc6e0a192)) {
        c6_public_compile_witness[14] = 1;
        return;
    }
    c6_public_compile_witness[15]++;
    if (arg->generation == 1) {
        ble_npl_event_deinit(ev);
        ble_npl_event_init(ev, public_callback, &second);
    }
}
void *c6p_example_init(void) {
    ble_npl_event_fn *checked = public_callback;
    ble_npl_event_init(&event, checked, &first);
    return &event;
}
void c6p_example_raw_cycle(void) {
    ble_npl_event_set_arg(&event, &second);
    if (ble_npl_event_is_queued(&event))
        c6_public_compile_witness[14] = 2;
    ble_npl_event_run(&event);
    ble_npl_event_deinit(&event);
}
void c6p_example_record(uint32_t index, uint32_t value) {
    if (index < 14) c6_public_compile_witness[index] = value;
}
uint32_t c6p_example_check(void) {
    return c6_public_compile_witness[14] != 0 ||
           c6_public_compile_witness[15] != 2 || event.event != NULL;
}
