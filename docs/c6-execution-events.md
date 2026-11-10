# Partial C6 execution service: nonblocking events

`internal/nplevent` supplies the common bounded Go event service used by the
[native adapters](native-event-services.md). It is not a usable controller/NPL
port or Matter commissioning backend. It does not register a controller,
change modem ownership, or change application admission gates.

## Implemented service

`New(capacity)` allocates one fixed pending-event ring during foreground setup.
`NewEvent(func() error)` owns a Go callback. Neither object may be copied.
`Put` suppresses a duplicate queued occurrence, rejects a full queue with
`ErrFull`, and rejects ownership by another queue with `ErrBusy`. `Remove`
cancels a pending occurrence without stopping an executing callback. The C6
binding uses TinyGo's saved-state interrupt disable/restore, including nested
critical sections; the HOST binding uses a mutex. Put/Remove have no explicit
allocation, callback, timer or task-wakeup path. Remove is bounded by capacity.

`Dispatch(0)` executes one callback on the foreground caller's existing Go task,
outside the service critical section. It never creates a native task or invokes
a foreign function pointer. Concurrent/reentrant dispatch is rejected with
`ErrBusy`. A running callback may enqueue itself once on the same queue, but
cannot transfer itself to another queue. Nonzero timeouts, including infinite
or negative waits, return `ErrUnsupported` without consuming an event.

`Close` seals admission and cancels pending events. A bounded cancellation hook
can reject detachment: that occurrence stays owned, Error retains the failure,
and Done remains open until cancellation succeeds. It returns `ErrBusy` while
a callback still owns its event. `Done` closes only after that callback actually
returns or unwinds, not when cancellation is requested. Waiting for Done from
that same callback would deadlock and is not supported. An idle event's
`Release` drops the callback reference; pending/running release returns
`ErrBusy`. Cancellation does not kill a Go task or release somebody else's
stack. Setup, dispatch, close and release reject ISR context using the pinned
TinyGo interrupt predicate.

Foreground callers must be existing Go tasks with interrupts and cache usable,
not foreign native task stacks. The binding restores the incoming interrupt
mask; it never forcibly enables interrupts. No cache-off/IRAM residency or C
entry trampoline is supplied by this slice.

Each dispatch returns its actual callback error. `Error` retains the first
failure, including a failure arriving after Close. Resource join and historical
failure are separate: query Error after Done. Panic/Goexit are not swallowed;
deferred ownership cleanup records `ErrAborted` rather than claiming normal
callback success. This is Go unwind behavior, not a C nonlocal-unwind contract.

## Why this is only part of NPL

The cached controller's NPL has duplicate event suppression and separate
task/ISR queue paths, but task-side put can wait indefinitely and get can block.
This service supplies **only the nonblocking ownership/dispatch substrate**.
It is not substituted into the vendor function table: such substitution would
misrepresent the missing operations.

The common queue itself does not provide:

- native C event layout/handle allocation, callback trampoline and foreign-stack
  rooting/unwind ABI (the separate adapters provide their documented subsets);
- native task create, task identity, blocked-C cancellation, delete and join;
- blocking queue get/put, put-to-front, waiter wakeup and ISR-to-task scheduling;
- semaphore/mutex waits, native time-unit conversion and timer/callout
  reset/stop/delete/join;
- controller ext_funcs/NPL registration, controller/host startup and shutdown;
- BLE/802.15.4 arbitration or a protocol-correct commissioning handover;
- GATT connection Write, C2 CCCD, negotiated MTU, indication confirmation,
  backpressure, disconnect generations and primary Matter advertisements.

There is deliberately no success-shaped controller factory, capability flag,
FreeRTOS TCB, forced goroutine kill or no-op coexistence hook. The common queue
needs no runtime patch; the separate owned-entry profile documents its overlay.
Calling a C function as TinyGo's compiler-generated goroutine wrapper would
violate the pinned RISC-V task ABI and is not attempted.

## Provider/admission boundaries

This queue is not an entropy-source ownership primitive. Queue completion
does not establish source quality, RF ownership, flash/XIP safety, storage
durability or a native memory-cap certificate. Existing driver checks and
application security/storage admission remain necessary and unchanged.

HOST tests exercise the common Go implementation, including contention,
callback ownership, cancellation/join, first/late failures and abnormal unwind.
They do not prove C6 ISR residency/latency, allocation lowering, native callback
ABI, scheduler-stack lifetime, RF, physical entropy, storage or Apple Home
readiness. Minimal adapter examples have been C6 compile/link checked only,
not executed or physically qualified.
