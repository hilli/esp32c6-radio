# Public C6 event adapter: supported subset and caller ownership

This adapter uses the **actual** `void(struct ble_npl_event *)` callback.
The generic `nimble_npl.h` is retained unmodified from the selected cached C6
source, with its checksum/Apache notice. Its `nimble_npl_os.h` is a new TinyGo
OS port: the public wrapper is one opaque pointer, as selected, but the
referenced cookie/private slots are **not FreeRTOS objects**. No controller
table is defined or registered; no private int32 callback is cast/relabelled.

Supported public signatures: event_init, event_get_arg, event_set_arg,
event_run, event_deinit and event_is_queued. The deinit OS extension matches
the selected `void(event*)` table member. All infallible public signatures
fail-stop on invalid use, preserving a native first-fault diagnostic.
Checked `c6_public_*` variants return actual NPL control errors, not callback
results: callbacks return void and may instead modify their own application
state. Invalid/stale/foreign wrapper copies never become successful NULL/empty
results. A NULL argument itself is legitimate.

## What is owned

The CALLER owns and initializes the public wrapper in native C memory and owns
every opaque arg pointee. The port owns only eight fixed private slots,
callback pointers, opaque pointer VALUES and generation-bearing references.
It never copies, frees or pretends to lease caller data. No arbitrary Go
pointer, dynamic code unloading, or retained callback-to-Go entry is supported.
Native callers must keep data valid for all actual accesses/arg borrowers;
retiring an event does not retire unrelated user-data borrowers.

The actual cached `ble_hs_event_rx_hci_ev` gets arg, deinitializes ev, returns
the wrapper to its pool, THEN processes the HCI arg before callback return.
The adapter supports that sequence: self-deinit detaches the wrapper immediately
but retains private callback state through real return. After calling fn(ev)
it touches only retained PORT state, never the caller wrapper or arg pointee.
The caller can free/protect the wrapper and finish consuming/freeing its arg.
An event can even be reinitialized at the same address during that old callback;
old completion cannot overwrite the replacement.

Concurrent reinit/run and foreign-thread active deinit/arg changes reject Busy.
Self set_arg/deinit are allowed within a real native invocation frame. Idle
reinit/deinit retires old generations even if a Go binding still holds a
private reference. Such a binding becomes stale, cannot schedule replacement
callbacks, and releases only old port storage. Slot generations never wrap;
exhausted generation space retires capacity rather than aliasing a stale ref.
Raw unversioned C pointer aliases cannot distinguish a caller's reused address:
ordinary C lifetime/quiescence remains mandatory. Only captured generation
references, not arbitrary expired raw addresses, have stale-reference fencing.

## Exact admitted context, not task identity

C APIs run entirely in C; none calls Go, blocks, allocates a task or dispatches
Go from an ISR. RV32 critical sections save/restore the actual MIE bit and
reject ISR/exception context using the same MCAUSE predicate as pinned TinyGo.
HOST uses a mutex and real thread-local C invocation frames.

On C6, the admitted context is a single stock TinyGo0.44 cooperative Go task's
C-only call stack: scheduler.tasks has no parallelism and yields explicitly.
Actual nested C invocation frames distinguish self retirement. This is NOT a
foreign-task ID primitive. Go reentry/yield/blocking from a callback, another
core, native task context switching, ISR entry and nonlocal C unwind are outside
the contract. A general scheduler/controller port cannot assume this subset.

The retained generic header sits beside adapter.h so its quoted
nimble/nimble_npl_os.h include is self-contained in TinyGo's declaration pass;
nimble/nimble_npl.h also forwards the standard include spelling. Public C
bool signatures remain unchanged. Only the private Go queue reservation
out flag uses uint32_t, avoiding the pinned frontend's C.bool macro/type issue.

## Genuine queue coupling, not a native eventq implementation

`Bind` takes a live caller-owned C event and captures/pins PORT state. Its Go
Put/Remove/Release are foreground-only. Common queue admission marks the native
queued reservation; dispatch consumes it before the actual C callback. Full or
failed admission rolls back only its own reservation. Duplicate insertion and
cross-queue ownership retain the common queue's existing behavior.

A narrowly added common cancellation hook makes Remove and Queue.Close retire
the native queued reservation before detaching Go ownership. A failed hook
retains the occurrence and keeps Done open; it is surfaced, not silently joined.
The hook is bounded, non-reentrant and called under the queue critical section;
this binding never exposes its event to an ISR remover. Done is callback return,
not caller-data release or foreign-task deletion/join.

No ble_npl_eventq get/put/front/block/wakeup, event_reset, task identity,
semaphore/mutex waits, callouts/timers, controller registration, coexistence,
GATT/BLE/RF or application/source/storage qualification is supplied. Unsupported native
families have no success-shaped definitions/table. A public header declaration
does not make them usable.

HOST fixtures use correctly typed actual C callbacks, native owned arg pointers
and real protected wrapper pages. They exercise same-address reuse, self
retirement, foreign rejection, pending cancellation, stale refs and caller
cleanup. The minimal C6 example is compile/link-only with no BIN/flash recipe.
Neither HOST nor an ELF proves target execution, ISR latency/cache-off safety,
general native task context, borrowed user-data health or Apple Home readiness.
