#include "adapter.h"

void *c6p_fixture_new(uint32_t mode, int32_t *status);
struct ble_npl_event *c6p_fixture_event(void *owner);
int32_t c6p_fixture_destroy(void *owner);
uint32_t c6p_fixture_calls(void *owner);
uint32_t c6p_fixture_issue(void *owner);
uint32_t c6p_fixture_live(void);
void c6p_fixture_wait(void);
void c6p_fixture_resume(void);
int32_t c6p_fixture_try(void *owner, uint32_t operation);
void c6p_fixture_public_cycle(void *owner);
uint32_t c6p_fixture_queued(void *owner);
void c6p_fixture_reinit(void *owner);
void c6p_fixture_arm_fault_exit(void);
