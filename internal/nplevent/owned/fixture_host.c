//go:build !tinygo && c6_owned_entry && c6_owned_hostfixture

#if !defined(__riscv)
#include "fixture.h"
#include "../publicabi/adapter.h"
#include <pthread.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <unistd.h>

struct fixture;
struct argument { struct fixture *owner; uint32_t cookie; };
struct fixture {
    struct ble_npl_event *event;
    struct argument *arg;
    uint32_t control, returns, detached;
    int32_t application;
    size_t page;
};
static pthread_mutex_t data_lock = PTHREAD_MUTEX_INITIALIZER;

static void callback(struct ble_npl_event *event) {
    struct argument *arg = ble_npl_event_get_arg(event);
    if (!arg || arg->cookie != UINT32_C(0xc6017ead)) __builtin_trap();
    struct fixture *owner = arg->owner;
    int32_t application = 0;
    uint32_t control = c6_owned_call_go(1, &application);
    /* Go may have retired/protected event and freed arg. Only the separate
     * caller-owned result object is touched after the typed export returns. */
    pthread_mutex_lock(&data_lock);
    owner->control = control;
    owner->application = application;
    owner->returns++;
    pthread_mutex_unlock(&data_lock);
}
void *c6o_fixture_new(void) {
    struct fixture *o = calloc(1, sizeof(*o));
    if (!o) return NULL;
    o->page = (size_t)sysconf(_SC_PAGESIZE);
    o->event = mmap(NULL, o->page, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON, -1, 0);
    o->arg = malloc(sizeof(*o->arg));
    if (o->event == MAP_FAILED || !o->arg) {
        if (o->event != MAP_FAILED) munmap(o->event, o->page);
        free(o->arg); free(o); return NULL;
    }
    *o->arg = (struct argument){o, UINT32_C(0xc6017ead)};
    if (c6_public_init(o->event, callback, o->arg)) {
        munmap(o->event, o->page); free(o->arg); free(o); return NULL;
    }
    return o;
}
void *c6o_fixture_event(void *p) { return ((struct fixture *)p)->event; }
uint32_t c6o_fixture_destroy(void *p) {
    struct fixture *o = p;
    if (!o) return BLE_NPL_EINVAL;
    if (!o->detached) {
        uint32_t rc = c6_public_deinit(o->event);
        if (rc) return rc;
    }
    free(o->arg);
    munmap(o->event, o->page);
    free(o);
    return 0;
}
uint32_t c6o_fixture_retire(void *p) {
    struct fixture *o = p;
    uint32_t rc = c6_public_deinit(o->event);
    if (rc) return rc;
    o->detached = 1;
    if (mprotect(o->event, o->page, PROT_NONE)) return 100;
    free(o->arg); o->arg = NULL;
    return 0;
}
uint32_t c6o_fixture_foreign_retire(void *p) {
    return c6_public_deinit(((struct fixture *)p)->event);
}
uint32_t c6o_fixture_control(void *p) {
    pthread_mutex_lock(&data_lock);
    uint32_t value = ((struct fixture *)p)->control;
    pthread_mutex_unlock(&data_lock); return value;
}
int32_t c6o_fixture_status(void *p) {
    pthread_mutex_lock(&data_lock);
    int32_t value = ((struct fixture *)p)->application;
    pthread_mutex_unlock(&data_lock); return value;
}
uint32_t c6o_fixture_returns(void *p) {
    pthread_mutex_lock(&data_lock);
    uint32_t value = ((struct fixture *)p)->returns;
    pthread_mutex_unlock(&data_lock); return value;
}
static void *foreign(void *p) {
    uint32_t *result = p;
    int32_t application = 77;
    *result = c6_owned_call_go(2, &application);
    if (application != 77) *result = 100;
    return NULL;
}
uint32_t c6o_fixture_foreign_call(void) {
    pthread_t thread;
    uint32_t result = 100;
    if (pthread_create(&thread, NULL, foreign, &result)) return 101;
    if (pthread_join(thread, NULL)) return 102;
    return result;
}
void c6o_fixture_raw(void *p) { ble_npl_event_run(((struct fixture *)p)->event); }
#endif
