//go:build !tinygo && c6_public_hostfixture

#if !defined(__riscv)
#include "fixture.h"
#include <pthread.h>
#include <stdlib.h>
#include <signal.h>
#include <sys/mman.h>
#include <unistd.h>

struct fixture_owner;
struct payload { struct fixture_owner *owner; uint32_t cookie; };
struct fixture_owner {
    struct ble_npl_event *event;
    struct payload *argument, *alternate;
    uint32_t mode, calls, issue, detached, protected;
    size_t page;
};
static pthread_mutex_t fixture_gate = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t fixture_changed = PTHREAD_COND_INITIALIZER;
static uint32_t entered, resumed, live;

/* The callback has the ACTUAL public signature; there is no int32 cast/status. */
static void public_callback(struct ble_npl_event *event) {
    struct payload *argument = ble_npl_event_get_arg(event);
    if (!argument || argument->cookie != UINT32_C(0xc6e0a192))
        __builtin_trap();
    struct fixture_owner *owner = argument->owner;
    if (event != owner->event || argument != owner->argument)
        owner->issue = 1;
    if (owner->mode == 1) {
        pthread_mutex_lock(&fixture_gate);
        entered = 1;
        pthread_cond_broadcast(&fixture_changed);
        while (!resumed)
            pthread_cond_wait(&fixture_changed, &fixture_gate);
        pthread_mutex_unlock(&fixture_gate);
    }
    if (owner->mode == 2) {
        /* Mirrors ble_hs_event_rx_hci_ev: retire/free wrapper inside fn,
         * continue consuming caller-owned arg, then return. Any adapter
         * wrapper access after this point faults, not just a freed-heap guess. */
        ble_npl_event_deinit(event);
        owner->detached = 1;
        if (mprotect(event, owner->page, PROT_NONE) != 0)
            owner->issue = 2;
        else
            owner->protected = 1;
        owner->calls += argument->cookie == UINT32_C(0xc6e0a192);
        free(argument);
        owner->argument = NULL;
        return;
    }
    if (owner->mode == 3) {
        ble_npl_event_deinit(event);
        owner->mode = 0;
        owner->argument = owner->alternate;
        owner->alternate = argument;
        /* Immediate same-address wrapper reuse while the OLD callback still
         * owns its retained port frame. Its eventual finish must not touch it. */
        ble_npl_event_init(event, public_callback, owner->argument);
    }
    owner->calls++;
}

void *c6p_fixture_new(uint32_t mode, int32_t *status) {
    struct fixture_owner *owner = calloc(1, sizeof(*owner));
    if (!owner) { *status = BLE_NPL_ENOMEM; return NULL; }
    owner->page = (size_t)sysconf(_SC_PAGESIZE);
    owner->event = mmap(NULL, owner->page, PROT_READ | PROT_WRITE,
                        MAP_PRIVATE | MAP_ANON, -1, 0);
    owner->argument = malloc(sizeof(*owner->argument));
    owner->alternate = malloc(sizeof(*owner->alternate));
    if (owner->event == MAP_FAILED || !owner->argument || !owner->alternate) {
        if (owner->event != MAP_FAILED) munmap(owner->event, owner->page);
        free(owner->argument); free(owner->alternate); free(owner);
        *status = BLE_NPL_ENOMEM; return NULL;
    }
    *owner->argument = (struct payload){owner, UINT32_C(0xc6e0a192)};
    *owner->alternate = *owner->argument;
    owner->mode = mode;
    *status = c6_public_init(owner->event, public_callback, owner->argument);
    if (*status) {
        munmap(owner->event, owner->page);
        free(owner->argument); free(owner->alternate); free(owner); return NULL;
    }
    pthread_mutex_lock(&fixture_gate); live++; pthread_mutex_unlock(&fixture_gate);
    return owner;
}
struct ble_npl_event *c6p_fixture_event(void *pointer) {
    return ((struct fixture_owner *)pointer)->event;
}
int32_t c6p_fixture_destroy(void *pointer) {
    struct fixture_owner *owner = pointer;
    if (!owner) return BLE_NPL_EINVAL;
    if (!owner->detached) {
        ble_npl_error_t rc = c6_public_deinit(owner->event);
        if (rc) return rc; /* real owner refuses to free any caller data */
    }
    free(owner->argument); free(owner->alternate);
    munmap(owner->event, owner->page);
    free(owner);
    pthread_mutex_lock(&fixture_gate); live--; pthread_mutex_unlock(&fixture_gate);
    return BLE_NPL_OK;
}
uint32_t c6p_fixture_calls(void *p) { return ((struct fixture_owner *)p)->calls; }
uint32_t c6p_fixture_issue(void *p) {
    struct fixture_owner *owner = p;
    return owner->issue | (owner->mode == 2 && !owner->protected ? 4 : 0);
}
uint32_t c6p_fixture_live(void) {
    pthread_mutex_lock(&fixture_gate);
    uint32_t count = live;
    pthread_mutex_unlock(&fixture_gate);
    return count;
}
void c6p_fixture_wait(void) {
    pthread_mutex_lock(&fixture_gate);
    while (!entered) pthread_cond_wait(&fixture_changed, &fixture_gate);
    pthread_mutex_unlock(&fixture_gate);
}
void c6p_fixture_resume(void) {
    pthread_mutex_lock(&fixture_gate); resumed = 1;
    pthread_cond_broadcast(&fixture_changed); pthread_mutex_unlock(&fixture_gate);
}
int32_t c6p_fixture_try(void *p, uint32_t op) {
    struct fixture_owner *owner = p;
    void *arg = NULL;
    switch (op) {
    case 0: return c6_public_deinit(owner->event);
    case 1: return c6_public_init(owner->event, public_callback, owner->alternate);
    case 2: return c6_public_set_arg(owner->event, owner->alternate);
    case 3: return c6_public_get_arg(owner->event, &arg);
    case 4: return c6_public_run(owner->event);
    default: {
        struct ble_npl_event copy = *owner->event;
        return c6_public_run(&copy);
    }
    }
}
void c6p_fixture_public_cycle(void *p) {
    struct fixture_owner *owner = p;
    /* Compile-time check against the actual retained declarations. */
    void (*init)(struct ble_npl_event *, ble_npl_event_fn *, void *) = ble_npl_event_init;
    void (*deinit)(struct ble_npl_event *) = ble_npl_event_deinit;
    init(owner->event, public_callback, owner->argument);
    ble_npl_event_set_arg(owner->event, NULL);
    if (ble_npl_event_get_arg(owner->event) != NULL) owner->issue = 8;
    ble_npl_event_set_arg(owner->event, owner->argument);
    if (ble_npl_event_get_arg(owner->event) != owner->argument) owner->issue = 16;
    if (ble_npl_event_is_queued(owner->event)) owner->issue = 32;
    ble_npl_event_run(owner->event);
    deinit(owner->event);
    owner->detached = 1;
}
uint32_t c6p_fixture_queued(void *p) {
    return ble_npl_event_is_queued(((struct fixture_owner *)p)->event);
}
void c6p_fixture_reinit(void *p) {
    struct fixture_owner *owner = p;
    ble_npl_event_init(owner->event, public_callback, owner->argument);
}
static void fault_exit(int signal_number) {
    (void)signal_number;
    _exit(64 + (int)c6_public_first_fault);
}
void c6p_fixture_arm_fault_exit(void) {
    signal(SIGILL, fault_exit);
    signal(SIGTRAP, fault_exit);
}
#endif
