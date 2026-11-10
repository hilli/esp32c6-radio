#include <stdint.h>

void *c6p_example_init(void);
void c6p_example_raw_cycle(void);
void c6p_example_record(uint32_t, uint32_t);
uint32_t c6p_example_check(void);
extern volatile uint32_t c6_public_compile_witness[16];
