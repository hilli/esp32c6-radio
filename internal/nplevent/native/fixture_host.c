//go:build !tinygo && c6_event_hostfixture

#if defined(C6_EVENT_TEST_FIXTURE) && !defined(__riscv)
#include "fixture_host.h"
#include <pthread.h>

struct fixture_argument {
    uint32_t cookie;
    int32_t status;
    uint32_t calls, block;
};

static pthread_mutex_t gate = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t changed = PTHREAD_COND_INITIALIZER;
static uint32_t entered, resumed, returned;

static int32_t fixture_callback(void *bytes, uint32_t length) {
    if (length != sizeof(struct fixture_argument) || (uintptr_t)bytes % 8)
        return -1001;
    struct fixture_argument *argument = bytes;
    if (argument->cookie != UINT32_C(0x6436c0de))
        return -1002;
    argument->calls++;
    pthread_mutex_lock(&gate);
    entered++;
    pthread_cond_broadcast(&changed);
    while (argument->block && !resumed)
        pthread_cond_wait(&changed, &gate);
    int32_t status = argument->status;
    argument->status = 0;
    returned++;
    pthread_mutex_unlock(&gate);
    return status;
}

uintptr_t c6_host_fixture_callback(void) {
    c6_event_callback checked = fixture_callback;
    return (uintptr_t)checked;
}

void c6_host_fixture_reset(void) {
    pthread_mutex_lock(&gate);
    entered = resumed = returned = 0;
    pthread_mutex_unlock(&gate);
}

void c6_host_fixture_wait_entered(void) {
    pthread_mutex_lock(&gate);
    while (!entered)
        pthread_cond_wait(&changed, &gate);
    pthread_mutex_unlock(&gate);
}

void c6_host_fixture_resume(void) {
    pthread_mutex_lock(&gate);
    resumed = 1;
    pthread_cond_broadcast(&changed);
    pthread_mutex_unlock(&gate);
}

uint32_t c6_host_fixture_returns(void) {
    pthread_mutex_lock(&gate);
    uint32_t count = returned;
    pthread_mutex_unlock(&gate);
    return count;
}
#endif
