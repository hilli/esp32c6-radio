# Software validation and remaining gates

Validation date: 2026-10-27. Host: macOS arm64. Toolchains: Go 1.27.3,
TinyGo 0.42.0 / LLVM 22.1.4. No board, serial monitor, flash operation or RF
test was used.

## Completed software checks

| Gate | Result |
|---|---|
| `make lint` | PASS: gofmt, `go vet ./...`, offline native SHA256/ROM binding checks |
| `make build` | PASS: `go build ./...` |
| `make test` | PASS: 62 top-level tests and their subtests with `-race -count=1` |
| Driver stress | PASS: `go test -race -count=100 .` |
| Settings stress | PASS: `go test -race -count=25 ./settings` |
| Native images | PASS: bring-up, raw A/B, old Thread link-only harness, runnable storage/entropy/RF/Thread probes |
| Image validation | PASS: ROM image checksum/hash, identity mapping, reserved partition/nonoverlap/4 MiB bounds, actual SRAM bridge instructions and relocations |
| Cold-cache consumers | PASS: isolated Go module/build caches, direct and vendored raw-radio **and native storage** images |
| Asset preservation | PASS: all native archives, headers, linker files and licenses compare byte-for-byte after vendoring |
| ISR allocation checks | PASS: 1000 host IRQ-path allocation probes report zero; TinyGo allocation diagnostics identify startup constructors, not the driver's IRQ/producer sites |
| Evidence tools | PASS: clearly synthetic fixture, missing evidence, identity/order/rollback rejection, RF witness channel/hash correlation, latched failures, private command permissions and Thread prerequisites |

These are local software results. The earlier published baseline also passed
normal module downloads and [Linux CI](https://github.com/hilli/esp32c6-radio/actions/runs/40533980232).
Current published-revision downloads/CI must be checked against their exact
commit; no earlier run is evidence for later code.

The private hardware boundary tests exercise the **actual C6 IRQ state machine**
against a fixed register model, not just a replacement Radio implementation.
They cover CCA opcode/duration, ACK wait/timeout/error identities, coalesced and
duplicate events, expected STOP outcomes, scan aborts, deferred configuration
during hardware ACK, source-address byte order, FCS/RSSI/LQI normalization and
buffer lifetime. Dispatcher tests cover queue pressure, reserved terminal slots,
overflow reporting, reentrant TX/Close, drain order and disable/re-enable.
The real gothread SubMac is also exercised across software CCA backoff and
frame retries.

The flash model enforces 1->0 word programs and sector geometry. Tests interrupt
every format/append/compaction program or erase boundary before, after and with
torn writes, then reboot. They cover whole-list/tombstone/Wipe atomicity, dirty
tails, readback failures, acknowledged monotonic reservations, retirement,
ambiguous commits, owner/schema/capacity/exhaustion and corruption. Real public
gothread secured MAC/MLE builders verify persisted dataset/counter restoration
and fail-stop reservation errors. Flash-pause regressions leave completion
callbacks queued on the calling stack loop, wait for hardware TX/automatic ACK,
and ensure a timeout neither mutates flash nor forcibly aborts that ACK.

Three deliberate, **compiling** mutations were applied and restored:

1. Replacing ACK-timeout `ErrNoAck` with success failed
   `TestMACACKTimeoutAndDuplicateEvents`.
2. Aliasing RX callback payloads to reusable storage failed
   `TestReceiveLifetimeAndMetadata`.
3. Removing the identity flash-offset check failed the nonidentity-image test.

The restored suite and native builds pass. An alternate-node build also exposed
a real four-byte rodata alignment gap; the linker now includes segment-header-
aware padding in `.text`, and linker assertions plus A/B image checks prevent
that invalid image from being treated as success.

Four additional compiling storage/evidence mutations were applied and restored:
missing old-bank retirement failed the acknowledged-compaction rollback test;
missing record CRC failed committed-record corruption; accepting synthetic
events failed the captured-fixture test; and moving the native bridge into XIP
produced real linked ELF/bin images that the SRAM verifier rejected. Compiler
or unapplied-patch failures were not counted as mutation results.

## Measured linked C6 images

Bytes, as checked from the actual ELF/bin pairs:

| Image | ROM image bytes | Static SRAM including IRAM/system stack | Remaining heap address space |
|---|---:|---:|---:|
| Bring-up probe | 47,040 | 23,936 | 428,176 |
| Directed raw node A | 102,784 | 25,472 | 426,640 |
| Directed raw node B | 103,168 | 25,728 | 426,384 |
| Storage campaign | 500,240 | 45,184 | 406,928 |
| Entropy campaign | 521,184 | 50,304 | 401,808 |
| RF campaign | 529,040 | 50,304 | 401,808 |
| Functional Thread campaign | 822,144 | 55,936 | 396,176 |

These are not nRF52840 numbers. The SRAM ceiling is `0x4086e610`; the system
stack reservation is 16 KiB. Remaining heap address space is **not live free
heap**: driver objects, task stacks, queues, message pools and Instance objects
consume it at runtime. TinyGo reports a 7240-byte driver object allocated at
startup, in addition to its device, closures and channels. The link-only harness
contains real attach/AES-CCM code but deliberately rejects storage and must
**never be flashed** as a functional Thread node.

The current local cold-cache raw consumer is 102,784 bytes; the storage consumer
is 500,224 bytes (45,184 static SRAM, 406,928 heap address space). Both direct
and vendored builds match. The storage consumer retains the native C/header/
linker bridge and passes actual SRAM control-flow verification.
Use `go run ./tools/check-consumer -version=<published-commit> -example=storage`
to repeat through normal Go module downloads, not a local replacement.

## Reproducible dependencies

Native PHY/baseband: ESP-IDF v5.5
`8c750b088c7cd857d079c0eeb495da199b359461`, with its coherent esp-phy-lib
revision `f6746ceea821b3800e8700d7e535f5df7304b8e5`. Both required archives are
real committed files under Apache-2.0 with original notices and exact hashes.

gothread is pinned to merged main revision
[`f61a13bebcdd740ca78807149f8677f30f19658f`](https://github.com/hilli/gothread/commit/f61a13bebcdd740ca78807149f8677f30f19658f),
including the separately coordinated persistence, TX metadata, scan-start
rollback and constructor identity fixes from
[hilli/gothread#3](https://github.com/hilli/gothread/pull/3).
No primary gothread working copy was edited by this implementation.

## Explicitly not completed

Physical durability remains a hard gate despite the implemented
[C6-specific backend](storage.md), source/ROM disassembly and machine-code proof.
The actual boot/ROM profile is unobserved, strict unsupported profiles fail, and
stuck ROM WIP loops are explicitly unbounded/fail-stop rather than fake timeouts.
No RAM/fixed-counter fallback is provided.

Actual calibration/IRQ execution, RX/TX, automatic ACK turnaround, directed
1000-frame reconciliation, secured attachment, bidirectional IPv6 echo,
power-cut/reboot recovery and the proposed 30-minute bounded-memory soak are
pending physical tests. Waveshare LCD work and its repeated soak remain deferred.
No release tag, certification, router qualification or hardware-ready claim is made.
The board has not arrived. All generated evidence remains **NOT_RUN**; see the
[ordered later lab campaign](lab-procedure.md). Entropy health smoke is not
cryptographic source qualification, and a private review artifact authorizes
only the isolated lab experiment, not production credential safety.
