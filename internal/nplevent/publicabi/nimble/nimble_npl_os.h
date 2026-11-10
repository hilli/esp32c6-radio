#ifndef C6_TINYGO_NIMBLE_NPL_OS_H
#define C6_TINYGO_NIMBLE_NPL_OS_H

/* TinyGo OS port, NOT the FreeRTOS backend. The public wrapper has the selected
 * C6 one-opaque-pointer layout. event is a non-dereferenceable port cookie,
 * not a FreeRTOS allocation. Unsupported OS types remain incomplete. */
typedef uint32_t ble_npl_time_t;
typedef int32_t ble_npl_stime_t;
struct ble_npl_event { void *event; };
struct ble_npl_eventq;
struct ble_npl_callout;
struct ble_npl_mutex;
struct ble_npl_sem;

/* This selected OS extension has the same void(event*) signature as the
 * cached p_ble_npl_event_deinit field; no npl_funcs table is supplied. */
void ble_npl_event_deinit(struct ble_npl_event *ev);

#endif
