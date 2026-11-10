# Optional Go-owned C entry and return/yield

This is selected explicitly with `c6_owned_entry`, and on C6 requires the exact
private TinyGo0.44 source overlay in `internal/nplevent/owned/runtime`.
It does not change installed TinyGo, normal targets, or the default C-only
no-reentry/no-yield event contract. No native scheduler/RTOS/tasks are added.

`owned.With(body, callback)` owns one synchronous native scope on the actual
current Go task. `c6_owned_call_go(request, int32_out)` is the guarded native
entry. The correctly typed private Go export is an implementation symbol,
not an unguarded C callback API. Native admission occurs before that export:
trap/exception, mask, actual runtime task, live scope and stack bounds are
checked. A request integer is an opcode, never an owner/qualification token.

## Real C6 identity and trap provenance

The pinned internal/task already exposes tinygo_task_current, but it does not
reject a system stack or trap. The optional C6-only SOURCE profile adds leaf
C ABI accessors using real Current(), OnSystemStack() and the typed runtime
state.canaryPtr. No struct offset/linkname is guessed; Task/state layout and
the scheduler are unchanged. Native scope storage keys the actual Task pointer,
checks the current native SP inside its live C scope/real stack lower bound,
and is cleared only after all native/export frames actually return.

The Go owner map strongly roots that real Task pointer and Go closures until
the native scope returns. Caller integers cannot install a C6 identity.
No arbitrary Go pointer is stored in user native event arguments.

Actual selected C6 handleInterrupt deliberately clears MCAUSE on normal IRQ
return and enables nesting. An inner IRQ can therefore leave MCAUSE zero while
an outer handler still executes. The optional C6 handler keeps a volatile
trap-depth counter from actual entry to return; exceptions never clear it.
The new gate combines that runtime-owned depth with the maintained CSR and
system-stack checks. It never force-enables MIE. Default runtime code remains
selected without the explicit tag. This source/compile evidence is not physical
ISR latency/cache-off or hardware trap-execution qualification.

## Ownership across Go yield

Native public-event frames use the real owner's frame root when With is live.
Another Go task sees its own root, not the suspended task's frames. A legitimate
C-to-Go callback may yield and reenter C metadata on its same actual owner;
foreign retirement stays Busy and self-deinit still supports caller wrapper/
argument retirement before the C callback returns. Captured generation refs
still protect stale port storage. Caller opaque pointees remain caller-owned.

Outside a live scope the original C-only frame root remains available, but a
native request for Go returns Unowned without a managed Go entry. ISR/exception
and masked managed entry are refused. No Go dispatcher/block/allocation occurs
from an unqualified ISR. Raw internal implementation symbols are not public
admission points.

Callback application int32 and entry-control result are separate; full Go
errors remain rooted and the first actual error is returned after native return.
Go panic is caught ONLY at the native ABI barrier and re-panicked with its
original value after C frames/scope close. No Go panic unwinds native C frames.
Goexit, nonlocal C unwind, arbitrary blocked/non-yielding C cancellation/join,
foreign task stacks and external context creation remain unsupported. A native
scope that has not returned does not authorize reclaiming its roots.

## HOST is not a C6 task proof

HOST uses a real Go scope, LockOSThread, native pthread TLS and a Go-created
runtime/cgo.Handle for GC lifetime. That handle is a rooted payload, not a C6
task identity or caller-issued permission. Foreign pthreads lack the native
scope and are rejected before the Go export. Tests use actual typed C-Go-C,
real Go yields/competing owners/GC, preserved first/late errors and protected
native wrapper/argument retirement. HOST success does not prove the C6 runtime.

The minimal C6 example retains the actual leaf runtime accessors, typed export,
two Go owners and real yield path. It is compile-only/non-flash and has not
been executed on C6. The [preparation guide](native-event-services.md) uses
`tools/prepare-owned-runtime` to verify pinned stock sources, apply the local
patch in a new private root, and verify overlay hashes. It never installs or
modifies the supplied toolchain. Set TINYGOROOT only for the explicit owned-entry
build; do not waive native-execution or application admission gates.

Controller/native task create/delete/preemption, blocking NPL queues/timers,
coexistence/GATT, application/source/entropy/storage qualification and all board/Home
claims remain separate, unavailable dependencies.
