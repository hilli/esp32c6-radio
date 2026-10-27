# Prepared lab campaign: board not yet available

**Current physical status: NOT_RUN for every gate.** Software models, synthetic
captures and linked firmware are not hardware observations. No attached device
has been inspected or operated. These commands prepare later work; do not select,
reset, flash, erase or modify a device until its exact identity and test scope
are explicitly approved.

## Required hardware and scope

Use the named Waveshare ESP32-C6-LCD-1.47/C6FH4 (4 MiB flash), a known-good
explicitly identified C6/OpenThread peer, a known-good 802.15.4 sniffer, reliable
USB/data power and a manually controlled disposable-board power connection.
No automatic power controller is assumed. The LCD stays disconnected/deferred.
Never use production credentials, an unrelated USB device or another network.

Before any destructive step, independently record the expected EUI64, chip
revision, actual flash identity/capacity, board model, serial device identity,
firmware build ID and physical scope. The scope includes:

1. Flashing only this disposable board's firmware.
2. Read-only backup of its complete original flash and the test partition.
3. Erasing/formatting **only** `0x3f8000` length `0x8000` when explicitly approved.
4. Controlled power interruptions during the named campaign.
5. RF on the explicit allowed channel/PAN/power and an isolated test dataset.

Backups can contain credentials. Keep them private/off-repo, mode 0600, hash
them, and never restore old credentials with older counter state. Full-flash
backup/restore/erase commands are deliberately not autoexecuted by our tools.
Use your explicitly approved port with the vendor flasher after reviewing its
arguments; no device discovery, reset or flash operation is hidden in `tools/lab`.

## Build and evidence preparation (safe now)

```sh
mise exec -- make check
mise exec -- make consumer
mise exec -- go run ./tools/lab build -probe storage -out build/lab/storage
```

The build command records exact source fingerprint, commit, native revision,
Go/TinyGo versions, firmware ID and ELF/bin SHA256. The exact image validator
also checks the partition and cache-off SRAM/ROM execution. Generated
`manifest.json` and `evidence.json` use mode 0600 and show physical NOT_RUN.
`make probes` builds storage, entropy, RF and functional Thread campaign images.
Unlike the old link-only harness, these endpoints are runnable, but none starts
destructive testing or transmits packets at boot.

When the board arrives, rebuild with the approved `-board` and `-expected-eui`.
Record independently observed `chip_revision` and `flash_id` in that manifest;
they are not guessed or set to a fake present-device default. `lab commands`
requires an explicit host boot/cut identifier. A firmware session cannot change
its run/boot identity without an actual new boot.

All device evidence is versioned `C6EVIDENCE ` JSONL with exact firmware,
run/boot and device identity. Unknown/private fields are rejected, raw entropy
and dataset/network-key bytes are never evidence fields. Capture serial output
to a private regular file, then:

```sh
go run ./tools/lab collect -manifest build/lab/storage/manifest.json \
  -input private-serial.log -out private-evidence.json
```

The collector never opens device files. Missing events, incomplete transactions,
wrong identity, duplicates and timeouts cannot become PASS. Clearly synthetic
`lab/testdata/synthetic-storage.jsonl` tests parsing only; every physical result
from a synthetic capture stays NOT_RUN.

## Storage campaign

With the radio disabled, first `hello`, then explicitly scoped `format` only if
the approved partition is completely erased. A corrupt/foreign partition
refuses formatting; there is no automatic erase recovery. Generate commands
without executing them:

```sh
go run ./tools/lab commands -manifest build/lab/storage/manifest.json \
  -boot boot-001 -operation hello
# Review RUN_ID/EUI from the manifest, not guessed values:
go run ./tools/lab commands -manifest build/lab/storage/manifest.json \
  -boot boot-001 -operation format -approve-scope EUI:0x3f8000:RUN_ID
go run ./tools/lab commands -manifest build/lab/storage/manifest.json \
  -boot boot-001 -operation write -counter 1 -delay-ms 1000 \
  -approve-scope EUI:0x3f8000:RUN_ID
```

Send only to the independently identified approved board. Keep the host's last
**ack** record outside the device. Preparation/checkpoint records are not
acknowledgements. Writes require strictly increasing disposable test counters.
Observers emit descriptor/payload, commit, retirement and erase checkpoints
outside cache-off execution; optional pauses make manual before-step cuts
reproducible. They do not pretend to give precise in-call power control.

Repeat with counters 1..1000 and enough traffic for multiple compactions.
For each named cut: preserve pre-cut acknowledgements, pull power manually at
the approved window, reboot, supply a fresh host boot ID, `hello`, then
`observe`. A successful recovery must be at least the host's last acknowledged
generation/counter. The independent ledger rejects rollback. Complete
unacknowledged new state is allowed; partial/corrupt state must fail closed.
Explicit ambiguity/corruption is an availability failure, never an old success.
Do not erase evidence or credentials to hide it.

Cover descriptor/payload programs, partial commit, after commit before host
acknowledgement, compaction copy/commit/retirement and each bank's sector erase.
True mid-program/erase manual cuts require repeated campaigns and independent
timestamps; no power-control fixture is invented. Measure program bytes,
erase sectors, compaction count, pause duration, recovery results and endurance
limits. A stuck ROM WIP loop requires an approved manual reset and is recorded
as missing completion/FAIL, not a passed native timeout.

## Entropy source and health

Build `-probe entropy`. After hello, explicitly approve
`EUI:phy-entropy:RUN_ID` for `-probe entropy -operation run`. Firmware performs
real PHY calibration/IRQ startup, reads configured FE/RNG clocks, and runs an
8192-byte streaming smoke test. It emits only a digest, counts, repetition/run
statistics and digital prerequisites, then clears the sample buffer.

Repeat across boots and continuous samples. Stuck, repeated, too-short or
grossly unbalanced samples fail; repeated boot digests fail the evidence check.
Missing PHY/FE/RNG prerequisites or Fill errors fail. The disabled-source path
must reject use rather than fall back to a timestamp/PRNG.

**These statistical checks do not prove cryptographic entropy.** The source
gate additionally needs review of the pinned C6 PHY/ADC path, exclusive ADC/
modem ownership, exact measured clock/PHY state, board/ROM revision and relevant
vendor assurance or independent analogue/source evidence. Capture at least two
source-condition artifacts and a substantive source/board review with hashes.
No public `entropyVerified=true` flag or passing smoke statistic certifies it.
The machine report deliberately leaves source qualification NOT_RUN.

Before creating even disposable Thread credentials, complete that source review
and physical storage campaign. `lab commands -probe thread -operation start`
requires physical identified storage evidence and a structured source review,
plus separately identified physical entropy-health evidence (`-entropy-evidence`),
not an unsafe boolean. This authorizes a disposable lab experiment, not a
production cryptographic-quality claim.

The private review JSON has `device_eui64`, exact `native_revision`,
`source_artifacts` (at least two hashed evidence references), and `review`
(substantive source/clock/analogue observations, at least 128 characters).
Do not fill it with a stock “approved” statement. A human must evaluate the
actual artifacts; the tool checks identity/completeness, not cryptographic quality.

## Explicit RF A/B campaign

Build `-probe rf-validation` for both known devices with the SAME host-generated
`-run-id` and their own `-expected-eui`. Supply channel, PAN and distinct local/
peer short addresses explicitly. Arm `EUI:rf:RUN_ID`, then send run commands to
both within the receive window. No packets are sent before that command.

Each direction requires 1000 accepted unicast attempts, exactly matching
started/terminal counts and reconciled success/noACK/CCA/abort counts. Frames
contain a unique 16-bit sequence and campaign tag, so unrelated traffic and DSN
wraps cannot be counted. RX unique/duplicates, signed RSSI offset representation,
LQI, invalid/unsupported/overflow and channel are recorded.

Keep a sniffer capture with FCS validation, actual address byte order and ACK
turnaround. The device never invents this artifact. A separately hashed
`lab.Witness` of kind `sniffer` supplies frame/FCS counts and maximum ACK time;
pass it with `collect -witness ... -artifact actual.pcapng`. Counter evidence
alone cannot pass the RF/FCS/timing gate. Repeat no-peer, busy-channel,
channel-mismatch, queue-pressure, disable/re-enable and close-during-TX cases.

## Functional always-on Thread campaign

Build `-probe thread-validation`; **never flash `tools/thread-link`** as a node.
Use a known-good explicit OpenThread router on an isolated disposable dataset.
Create a fresh dataset on that reference peer only after the storage/source
preconditions above. Do not copy a production dataset or fixed public key.

The endpoint opens the real C6 backend, shares its Radio with
`threadnode.RunWithDriver`, checks persistence/identity/dataset errors, selects
an always-on minimal end device and refuses unsafe missing scope. First
provision via a private dataset hex file; later boots omit it and restore.
`lab commands` requires `-out-command` mode-0600 output for private dataset
commands and never echoes their contents to stdout/evidence.

Approve `EUI:thread-lab:RUN_ID`; provide the exact reference-peer IPv6 address,
60..3600 seconds duration, storage-campaign, entropy-health and source-review
artifacts. For first provisioning (placeholders must be replaced with actual
approved identities/artifacts):

```sh
go run ./tools/lab commands -manifest build/lab/thread-validation/manifest.json \
  -probe thread -operation start -boot thread-boot-001 \
  -approve-scope EUI:thread-lab:RUN_ID -peer-ipv6 APPROVED_PEER_IPV6 \
  -duration 1800 -storage-evidence private-storage-evidence.json \
  -entropy-evidence private-entropy-evidence.json \
  -source-review private-source-review.json -dataset-file private-dataset.hex \
  -out-command private-thread-command.json
```

Later boot commands omit `-dataset-file` and restore through the Instance;
retain all other identity/safety inputs. Neither command generation nor
collection sends anything to a device.
The runnable monitor records role/secured TX/counters and actual ICMPv6 requests/
replies, drives real outbound pings, samples live heap and radio/flash pauses,
and handles a missing peer/channel mismatch with an explicit 90-second attach
failure. Capture reference-router echo replies independently in a hashed
`reference-peer` witness. The host checks counter observations across actual
boots before the Thread gate can pass.

Use 1800 seconds for the proposed attached soak. Any detach fails; retained-heap
growth after warmup must stay within the explicitly declared 32 KiB limit, with
peak/endpoint memory recorded. This threshold is an acceptance choice, not a
claim that host preallocation proves bounded device memory.

Power-cut/reboot during reservations/compaction and missing-peer/channel-negative
tests remain required. LCD is optional and deferred until all core gates pass;
then keep Waveshare brightness <=50% and repeat the campaign/soak with rendering
outside the radio ISR, callback worker and Thread event loop.
