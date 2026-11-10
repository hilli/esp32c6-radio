#include <stdint.h>

void *c6o_example_init(void);
uint32_t c6o_example_retire(void);
uint32_t c6o_example_foreign_retire(void);
uint32_t c6o_example_check(void);
void c6o_example_record(uint32_t, uint32_t);
extern volatile uint32_t c6_owned_compile_witness[16];
