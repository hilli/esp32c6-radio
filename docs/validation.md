# Software validation and remaining gates

Validation date: 2026-10-27. Host: macOS arm64. Toolchains: Go 1.27.3,
TinyGo 0.42.0 / LLVM 22.1.4. No board, serial monitor, flash operation or RF
test was used.

## Completed software checks

| Gate | Result |
|---|---|
| `make lint` | PASS: gofmt, `go vet ./...`, offline native SHA256/ROM binding checks |
| `make build` | PASS: `go build ./...` |
| `make test` | PASS: 29 top-level tests and their subtests with `-race -count=1` |
| Driver stress | PASS: `go test -race -count=100 .` |
| Native images | PASS: bring-up, raw A, reversed-address raw B, Thread link-only harness |
| Image validation | PASS: RISC-V ELF, required real native symbols, C6 ROM image headers/payloads/checksum/hash, identity flash mapping and SRAM bounds |
| Cold-cache consumer | PASS: isolated Go module/build caches, local module replacement, direct and `go mod vendor` TinyGo builds |
| Asset preservation | PASS: all native archives, headers, linker files and licenses compare byte-for-byte after vendoring |
| ISR allocation checks | PASS: 1000 host IRQ-path allocation probes report zero; TinyGo allocation diagnostics identify startup constructors, not the driver's IRQ/producer sites |
| Published module download | PASS: normal Go download of implementation commit `68d80c25542c82df9de1911bfb023e8f35db8adc`, cold Go caches, direct and vendored TinyGo builds |
| Hosted Linux CI | PASS: [Software gates run 40533980232](https://github.com/hilli/esp32c6-radio/actions/runs/40533980232), including `make check` and `make consumer` |

The private hardware boundary tests exercise the **actual C6 IRQ state machine**
against a fixed register model, not just a replacement Radio implementation.
They cover CCA opcode/duration, ACK wait/timeout/error identities, coalesced and
duplicate events, expected STOP outcomes, scan aborts, deferred configuration
during hardware ACK, source-address byte order, FCS/RSSI/LQI normalization and
buffer lifetime. Dispatcher tests cover queue pressure, reserved terminal slots,
overflow reporting, reentrant TX/Close, drain order and disable/re-enable.
The real gothread SubMac is also exercised across software CCA backoff and
frame retries.

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

## Measured linked C6 images

Bytes, as checked from the actual ELF/bin pairs:

| Image | ROM image bytes | Static SRAM including IRAM/system stack | Remaining heap address space |
|---|---:|---:|---:|
| Bring-up probe | 47,040 | 23,936 | 428,176 |
| Directed raw node A | 102,288 | 25,472 | 426,640 |
| Directed raw node B | 102,672 | 25,728 | 426,384 |
| Thread wiring **link-only** harness | 511,184 | 45,440 | 406,672 |

These are not nRF52840 numbers. The SRAM ceiling is `0x4086e610`; the system
stack reservation is 16 KiB. Remaining heap address space is **not live free
heap**: driver objects, task stacks, queues, message pools and Instance objects
consume it at runtime. TinyGo reports a 7240-byte driver object allocated at
startup, in addition to its device, closures and channels. The link-only harness
contains real attach/AES-CCM code but deliberately rejects storage and must
**never be flashed** as a functional Thread node.

The published-code consumer check used
`go run ./tools/check-consumer -version=68d80c25542c82df9de1911bfb023e8f35db8adc`.
Both downloaded-module and vendored images were 102,288 bytes with 25,472 bytes
of static SRAM and 426,640 bytes of heap address space. All imported native
assets and their notices survived the normal Go module zip and vendoring paths.
Subsequent validation-report updates do not change that implementation.

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

Durable C6 Settings are a hard gate. TinyGo 0.42 has no C6 Flash driver; the
[storage investigation](storage.md) identifies a source-supported ROM route but
does not pass off symbol linkage as cache-safe, power-loss-safe persistence.
The Thread wiring package requires a verified durable backend and physical PHY
entropy validation; no RAM/fixed-counter fallback is provided.

Actual calibration/IRQ execution, RX/TX, automatic ACK turnaround, directed
1000-frame reconciliation, secured attachment, bidirectional IPv6 echo,
power-cut/reboot recovery and the proposed 30-minute bounded-memory soak are
pending physical tests. Waveshare LCD work and its repeated soak remain deferred.
No release tag, certification, router qualification or hardware-ready claim is made.
