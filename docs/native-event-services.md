# Experimental native event services

These internal services are implemented building blocks, not a Bluetooth
controller/NPL port or Matter commissioning backend. They do not start RF,
provide coexistence, or change radio, entropy, security or storage admission.
HOST behavior and minimal C6 compile/link checks are not device execution or
Thread/Home pairing evidence.

| Layer | Contract |
|---|---|
| [Common events](c6-execution-events.md) | Fixed-capacity Go handoff, foreground dispatch, cancellation/actual-return join, first/late errors |
| [Private C bridge](c6-private-event-abi.md) | Static `int32(void*,uint32)` callback, copied 32-byte argument, generation handles; not the NPL ABI |
| [Public events](c6-public-event-adapter.md) | Real `void(ble_npl_event*)`, caller-owned wrapper/arg pointees, self-retirement and stale generation refs |
| [Owned entry](c6-go-owned-entry.md) | Explicit `c6_owned_entry` scope, real Go-task identity and guarded typed C-to-Go return/yield |

Callbacks/objects follow each layer's lifetime contract. No task is forcibly
cancelled, no caller pointee is silently owned/freed, and no partial controller
function table advertises unsupported operations. Public infallible C event
signatures fail-stop on invalid use; checked forms return control errors.

## HOST checks

Use the existing project Go baseline, cgo, a C compiler and Git. No SDK or new
Go dependency is required for these services. Native fixtures are explicit:

```sh
GOMAXPROCS=2 go test -p=2 -race \
  -tags=c6_event_hostfixture,c6_public_hostfixture ./internal/nplevent/...
GOMAXPROCS=2 go test -p=2 -race \
  -tags=c6_owned_entry,c6_owned_hostfixture,c6_event_hostfixture,c6_public_hostfixture \
  ./internal/nplevent/...
go test ./tools/prepare-owned-runtime
```

Tests run real C callbacks, protected caller wrapper pages, actual Go yields,
competing HOST owners, foreign/unowned refusal, generation/cancellation
ownership, late errors and cleanup. HOST uses real native-thread TLS and Go
GC handles; it is not a C6 runtime-identity or complete C sanitizer proof.

## Compile-only C6 examples

The existing ordinary radio `mise.toml` pin is unchanged. These separate
platform examples use an **already installed TinyGo 0.44.0**, Go 1.27.3 and
the concrete XIAO C6 runtime only for machine definitions. They do not configure
board pins or import/start the radio PHY/controller. Do not flash or execute
them based on compile evidence. No BIN/programming recipe is provided.

Choose the existing 0.44.0 binary explicitly rather than the ordinary radio
toolchain shim:

```sh
TINYGO_044=/path/to/tinygo-0.44.0/bin/tinygo
mkdir -p build
"$TINYGO_044" build -p 2 -target ./targets/esp32c6-event-abi.json \
  -o build/native-event-abi.COMPILE-ONLY.elf ./examples/native-event-abi
"$TINYGO_044" build -p 2 -target ./targets/esp32c6-event-abi.json \
  -o build/public-event.COMPILE-ONLY.elf ./examples/public-event-abi
```

Owned entry needs the narrowly pinned optional runtime SOURCE overlay. The
preparer checks the source ABI and patch before creating output, copies only
the modified packages, uses the remaining stock sources/libraries read-only,
applies the local patch and checks every overlay result. No tool download,
install/global configuration, compiler or device operation is performed.

```sh
TINYGO_ROOT="$("$TINYGO_044" env TINYGOROOT)"
go run ./tools/prepare-owned-runtime \
  -source "$TINYGO_ROOT" -output build/tinygo-owned
TINYGOROOT="$PWD/build/tinygo-owned" "$TINYGO_044" build -p 2 \
  -target ./targets/esp32c6-owned-entry.json \
  -o build/owned-entry.COMPILE-ONLY.elf ./examples/go-owned-entry
```

The output parent must exist and the output itself must not exist or be under
the supplied TinyGo root. A failed preparation leaves its new partial output
for inspection; it never deletes or overwrites an existing runtime/output.
The stock source hashes are platform-independent; there is no machine-specific
compiler-binary hash or private SDK replacement in the module.

The owned profile changes only a private C6 source copy. Task/state layout and
the scheduler are unchanged; current-task/stack/trap accessors are typed against
the pinned source. A depth counter complements the runtime-maintained MCAUSE
predicate during nested IRQs. Installed/default targets are not patched.

## Remaining boundaries

Native task create/delete/preemption, foreign-stack cancellation/join, blocking
NPL queues/timers, controller registration, BLE/802.15.4 arbitration/handover,
GATT/indication/MTU/disconnect/advertisements and physical source/storage/RF
qualification remain unavailable. The driver still owns the modem exclusively.
No native memory-cap, cache-off/ISR-latency, usable BLE backend or Apple Home
readiness follows from these services or examples.
