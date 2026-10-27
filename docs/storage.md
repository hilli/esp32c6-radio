# Durable ESP32-C6 storage: unclosed gate

No persistent ESP32-C6 Settings backend is provided or claimed durable. The
Thread wiring example rejects nil Settings and never substitutes RAM.
Physical entropy validation is also required before credentials are provisioned.

## Source evidence

TinyGo **0.42.0** at `bdc4a21af007d8f059b5cefe5644b7ee7bda5b2d`
only has an Espressif flash block device in
`src/machine/machine_esp32c3_flash.go` (`//go:build esp32c3`). Its common
`src/machine/flash.go` build expression also excludes C6. C3's DROM translation
is `0x3c000000`, not this module's C6 identity map at `0x42000000`; copying that
driver unchanged would target the wrong flash offsets.

The source-supported route fits the existing architecture without an IDF
runtime: a small C6-specific ROM bridge plus a pure-Go crash-safe Settings
journal. ESP-IDF **v5.5**, commit
`8c750b088c7cd857d079c0eeb495da199b359461`, provides these documented C6
ROM entries in `components/esp_rom/esp32c6/ld/esp32c6.rom.ld`:

| Entry | ROM address | Constraint / use |
|---|---|---|
| `esp_rom_spiflash_unlock` | `0x40000154` | Check its result, never assume unlock succeeded |
| `esp_rom_spiflash_erase_sector` | `0x40000144` | Sector number; 4096-byte erase units |
| `esp_rom_spiflash_write` | `0x4000014c` | Address, source and length word-aligned; pre-erase required |
| `esp_rom_spiflash_read` | `0x40000150` | Address, destination and length word-aligned |
| `esp_rom_spiflash_wait_idle` | `0x40000110` | Completion/status semantics must be honored |
| `Cache_Invalidate_Addr` | `0x40000648` | Invalidate the affected mapped C6 range |

Declarations and return codes are in
`components/esp_rom/esp32c6/include/esp32c6/rom/spi_flash.h`.
These addresses are **not C3 addresses** and are not added as speculative
flash-call stubs to this driver.

## What a real port must establish

1. Reserve an explicit, erase-aligned flash partition in the linker and image
   verifier. The current target allows the whole 4 MiB for firmware and reserves
   **no** Settings partition. Free space in today's small image is not a
   reservation. A future two-sector minimum at the end of flash must also
   account for image integrity/trailers and any actual boot/partition layout.
2. Keep the flash transaction, all accessed data/literals and required return
   path in SRAM while caches are unavailable. Prove the C6 ROM guard/cache
   initialization requirements, not merely symbol resolution. Check unlock,
   write, erase and completion results; bound operations to the partition,
   including overflow, zero-length and alignment cases.
3. Coordinate with the radio. An erase or cache-disabled write cannot let the
   current flash-resident Go ISR execute, nor block pending-bit decisions during
   legacy ACK turnaround. Stop/quiesce radio work or prove an IRAM-safe path.
   Never write while an accepted TX/DMA buffer still belongs to hardware.
4. Implement atomic durable `Set`, not in-place overwrite. An A/B or append-only
   journal needs sequence/CRC/commit validation, recovery without rollback,
   safe compaction, tombstones and exclusive namespace ownership. A power cut
   may expose the old or the new complete value, never a partial success.
5. Test simulated interruption at every program/erase/commit step, and then
   physical cuts on the actual C6FH4 flash across writes and compaction.
   Readback through cache alone is not evidence that a successful `Set`
   survives power loss.

## Generic gothread support

The pinned, merged gothread revision `f61a13bebcdd740ca78807149f8677f30f19658f`
includes Settings-backed active/pending datasets, key sequence and
reserve-before-encryption MAC/MLE counters. Use
`inst.ActiveDataset().SaveLocalFromInfo(&info)` for fresh provisioning and check
its error, `PersistenceError`, `Start`, `IfConfigUp` and `ThreadStart`.
Dataset-only credentials without counter history are rejected.
It also forwards TX channel/CCA metadata, rolls back rejected scan starts, and
initializes the injected radio's canonical factory identity before dependent
constructors capture it.

`Settings.Get` must distinguish absent data (`platform.ErrSettingsNotFound`)
from I/O/corruption failures. `Set` must be atomic and durable **before returning
success**. Never restore an older namespace snapshot, share it between live
instances, or erase only counters while retaining credentials. MAC encryption
stays in gothread; this driver does not advertise `CapTransmitSec`.

The generic persistence and metadata changes were made independently in
gothread. No user's primary working copy was edited here. Neither those host
tests nor a linked C6 ROM port would close the physical storage gate.
