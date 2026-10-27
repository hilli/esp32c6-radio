# Experimental durable C6 Settings

The internal-flash backend and crash-safe journal are implemented and
software-tested. **Physical durability is NOT_RUN: the C6 board has not arrived.**
No device has been inspected, selected, reset, flashed, erased or operated.
Do not treat successful host tests, readback or a linked port as physical proof.

## API and ownership

```go
driver, err := esp32c6radio.New()
if err != nil { return err }
// Check every error. Initial formatting is a separate, explicitly authorized
// disposable-partition operation while the radio is disabled.
store, err := settings.Open(driver)
if err != nil { return err }
defer store.Close()
// For Thread, share this SAME driver:
return threadnode.RunWithDriver(driver, store, completeLabDataset, stop, observe)
```

`settings.Open` never autoformats or erases. `ErrUnformatted` means the complete
reserved partition is erased; corruption, ambiguous commits, wrong EUI64 or
schema and read errors are different errors. `settings.FormatEmpty(driver)`
requires a disabled radio and an entirely erased partition. It does not erase
corrupt credentials/counters as recovery. Only one native Store owns the
partition at a time; the namespace is bound to the canonical factory EUI-64.

`Get`, `Set`, `Add`, `Delete` and `Wipe` implement `platform.Settings`.
`Set` replaces the whole list atomically; `Delete(key,-1)` removes all values.
Invalid negative indices are not missing-data errors. Returned bytes are copies.
Only genuine absence returns `platform.ErrSettingsNotFound`.

Limits are explicit: 16 keys, 32 total values, 512 bytes/value and 4096 bytes of
canonical live state. Exhaustion returns an error without modifying flash.
Generation and bank epoch are uint64; they never wrap. Wipe atomically clears
the **whole** namespace and requires the native radio disabled. Stop/discard the
old Instance first; provision a new disposable dataset. Reusing an old key after
erasing counters or restoring an older flash backup is unsafe.

## Partition and protocol

The 4 MiB target reserves physical **`0x3f8000..0x400000`**, exactly 32 KiB,
erase-aligned and excluded from firmware FLASH. Both linker symbols and the
ELF/bin validator enforce real nonoverlap, image trailers and physical bounds.
It consists of two 16 KiB banks (four 4096-byte sectors each).

Each bank has a versioned owner/epoch/base-generation header, CRC-protected
canonical snapshot, 128 fixed transaction descriptors and an append payload
arena. A transaction replaces one whole setting/list; count zero is a tombstone.
Header and payload are programmed/read back before the descriptor's independent
commit word. No multiword program is assumed atomic.

Fixed descriptor positions make a corrupted committed length distinguishable
from an unfinished tail. Exact committed records must pass header/payload CRC,
bounds, list structure and consecutive generation checks. A partial commit word
is ambiguous and fails closed, not a reason to return an older success.
An unfinished uncommitted tail forces compaction rather than reuse.

Compaction writes and commits the other bank's complete new snapshot first,
then retires the old bank, and only then acknowledges success. Retirement is
at the **last word** of the old bank and erased last. A damaged newer
acknowledged bank cannot silently revive a stale retired bank. Interrupted
program/compaction can recover the old or the complete new state; ambiguous or
corrupt outcomes stop with an error. Availability is not preferred over counter
safety. Every uncertain mutation latches the live handle until close/reopen.

CRC protects accidental integrity, not a malicious actor replaying a complete
valid older partition image. C6 flash alone is not a trusted monotonic counter.
Exclusive namespace/no-rollback operation remains a platform precondition.

## Native execution proof and restrictions

The small ROM bridge uses ESP-IDF v5.5
`8c750b088c7cd857d079c0eeb495da199b359461`, not C3 DROM translation.
Its C6 ROM entries are unlock `0x40000154`, write `0x4000014c`,
erase-sector `0x40000144`, wait-idle `0x40000110`, cache-suspend `0x40000698`,
invalidate-all `0x4000064c` and resume `0x4000069c`.

Read-only research also disassembled Espressif's official
`esp-rom-elfs-20260528` C6 rev0 ROM ELF:

- Release archive SHA256:
  `caa463d3cbef2430a5a35847c1d9f2f152403b17a802050927ff60c8da54fe46`.
- C6 ELF SHA256:
  `788e1d38724aeb8fd974fa10c4a7b089c02627d35342ce84b9e0b12b239f3551`.
- The public legacy wrappers dispatch installed function-table overrides or
  their actual ROM fallback. The backend **rejects** nonzero overrides/OS guard
  hooks, invalid SRAM table pointers, unsupported geometry and unknown capacity.
  It does not replace unknown functions with no-op stubs.
- The profile requires 4096-byte sectors, 256-byte pages, 64 KiB blocks,
  at least the reserved 4 MiB mapping and the pinned SDK's 4 MiB JEDEC capacity
  code `0x16`, ROM read/program sub-blocks of 16/32 bytes and supported 23/24
  address-bit fields. An observed board/ROM profile mismatch is an explicit gate, not
  permission to patch guard state until it appears successful.

All cache-off instructions, literals, buffers and return path are SRAM/ROM.
The actual ELF verifier decodes RV32IMC call/branch transfers, resolves
AUIPC/JALR pairs, rejects XIP/unresolved calls and XIP relocations, and requires
`--emit-relocs`. Disassembly exposed a real incorrect absolute-call encoding;
ROM bindings now live in the linker, not inline assembler aliases.

Programs are range/alignment checked and split at 256-byte page boundaries;
erases are bounded to the dedicated banks. Native return statuses and completion
are checked. Mapped reads after unified-cache invalidation avoid the documented
C6 ECO0 ROM-read workarounds, including their otherwise unsafe dummy write.

**Known native fail-stop:** audited ROM WIP/status loops are unbounded if flash
is absent/stuck. TinyGo disables its watchdogs. This port cannot manufacture a
software timeout while trapped inside those ROM calls with XIP suspended:
it returns no success, and independent host observation must fail/timeout.
A failed completion check stays in SRAM until an approved manual reset.
This limitation is explicit and still requires physical failure testing.

## Radio and runtime integration

`Radio.WithFlashAccess` serializes mutation and first quiesces MAC/DMA with
interrupts **enabled**. It prevents new TX/scan/configuration restarts, allows
already accepted TX or automatic ACK to finish via the real IRQ, and waits only
for hardware ownership to end. It never waits for a completion callback queued
on the same Thread loop running `Settings.Set`. Receive is restored afterward.
The narrow C bridge masks interrupts only after quiescence and restores cache
before returning into Go/XIP. No FreeRTOS/IDF runtime is embedded.

Scans/foreign ownership or a quiescence timeout return an explicit busy/error
without mutation. Settings must not be used concurrently with independent
flash writers, Wi-Fi/BLE, LP-core radio control or an altered SPI/ROM profile.
Erase/program pauses lose RF receive opportunities; peers may retry.
Pause count/time and program/erase/compaction statistics are observable.
All successful Set acknowledgements follow completion, commit and readback;
those software properties still do not prove power-loss durability.

Endurance is not infinite: append avoids erase-per-reservation, but compaction
erases four sectors and copies live state. Record actual bytes/sectors/pauses
over the lab traffic rate. No unknown flash chip's endurance specification or
service lifetime is invented here.

## Acceptance

Host tests model 1->0 word programs, sector erase, every operation boundary,
before/after/partial errors, ambiguous commits, compaction and reboot, ownership,
capacity, generation exhaustion and corruption. Actual public gothread frame/
MLE builders verify dataset/counter restoration and fail-stop reservations.
See [the runnable lab procedure](lab-procedure.md) for controlled cuts,
independent acknowledgement witnesses and remaining physical gates.
