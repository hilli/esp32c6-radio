# Private native event bridge - not the vendor NPL ABI

`internal/nplevent/native` adds a C function-pointer/argument boundary to the
common event service. Its private callback is
`int32_t (*)(void *argument, uint32_t length)`. It is **not** the cached NPL's
`void (*)(struct ble_npl_event *)`, nor its one-void-pointer event wrapper.
No FreeRTOS object, vendor event allocation or controller table is fabricated.

## Ownership and native return

New accepts only an address of statically linked C code with that exact
signature. Never pass a Go function value, unloaded dynamic-library code,
vendor callback of another signature or arbitrary address. The bridge cannot
validate arbitrary machine code. A typed C assignment checks both supplied
fixtures' actual signatures before exposing their code addresses.

Eight fixed C slots each own at most 32 copied, 8-byte-aligned argument bytes.
New retains no Go pointer or caller-buffer address in C. The callback may
modify its owned bytes but only borrow their address through its return; it
must not retain an alias, start a foreign worker using it, call Go, block on
another TinyGo task, or unwind nonlocally. There is no native task stack.

The private handle is index+generation, two uint32 fields, not a pointer or a
FreeRTOS TCB. Lookup rejects freed/reused/out-of-range handles. Generation
never wraps: a UINT32_MAX slot is permanently retired on the next allocation.
No finalizer reclaims slots; callers must explicitly Release idle events.
Dropping a Go owner can leak a bounded slot, never make an old handle valid.

Put/Remove use the actual common event queue, with no C call from its ISR path.
Queued/running Release returns Busy before native reclamation. Dispatch
claims the C slot under the bridge critical section, restores the caller's
mask, and invokes the real C trampoline on the existing Go task's call stack.
The C trampoline sets its returned flag only after the actual callback returns.
Finish rejects an unconfirmed return; release additionally rejects a claimed
slot. No service lock is held during the callback.

All private C metadata APIs except Invoke require the Go bridge's serialized
critical section: saved TinyGo interrupt state on C6, mutex on HOST. They are
not an independently callable concurrent native port. The C callback owns its
status/argument/returned fields exclusively; status queries reject a running
slot. There is no partial NPL table or C-to-Go ISR entry.

Nonzero signed32 callback status becomes a data-bearing CallbackError. The C
slot and common queue independently preserve their first actual failure;
each dispatch returns its actual result. Historical errors survive native
slot release in the queue's Go error value. A failure before confirmed return
does not authorize reclaiming native storage. Queue Done is callback completion
only, never foreign-task deletion/join.

Foreground setup/dispatch/release require a real Go task with usable interrupts
and cache. The existing interrupt mask is restored, not forcibly enabled.
Native task creation/block/delete/join, blocking queues/wakeup/front insertion,
sync waits, timer/callout lifetime, controller startup/coexistence, GATT/BLE/RF
and all source/Store admission qualification remain unavailable.

## Validation boundary

HOST requires existing cgo and a C compiler. `c6_event_hostfixture` explicitly
enables real C callbacks and a pthread gate fixture, not a controller. Tests
cover copied/aligned mutable argument lifetime, actual return before release,
late signed status, cancellation, stale/reused handles, exhaustion/cleanup and
generation retirement. A HOST-only near-wrap fixture reaches the real maximum
generation arm; that fixture is excluded from C6.

`examples/native-event-abi` is **compile/link-only, not for flashing/execution**.
Its statically linked C callback, private bridge descriptor and volatile witness
retain the actual callback invocation, C6 interrupt binding, first status and
release path. The descriptor reports actual C sizes, not a host size projected
onto C6. No BIN/programming recipe is supplied. An ELF is not proof of target
callback execution, ISR latency/residency/cache-off safety or usable BLE.
The compile-only target inherits the pinned concrete xiao-esp32c6 runtime
directly, caps linker/LTO jobs at two, and imports neither the PHY nor controller.
It is not the module's RF/storage linker profile or a board-pin qualification.

This bridge is independent of the public adapter. Neither supplies a full
native task/controller contract or qualifies application boot/storage gates.
