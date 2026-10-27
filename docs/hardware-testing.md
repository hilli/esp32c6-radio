# Physical acceptance procedure

**All physical gates below are pending.** No supplied binary has been run on a
board. A successful host test, calibration call's presence in an ELF, or image
layout check is not a successful hardware calibration, interrupt, or RF test.

Use an RF-appropriate lab setup and locally permitted channel/power. Keep
credentials and captures containing real network secrets out of public commits.
Record board revision, C6 chip revision, firmware commit, toolchain versions,
power source, antennas, channel, power, peer firmware revision and sniffer.

## 1. Bring-up and interrupt gate

Build `examples/bringup` with the supplied target. A board must actually report
the PHY/baseband initialization return, `MAC timer interrupt observed`, and
millisecond uptime advancing at the expected rate. Verify USB serial reception
and runtime timer behavior independently. Capture failures, resets and logs;
the IRQ probe has an explicit timeout, not a success fallback.

Confirm the factory EUI-64 against the eFuse/known-good vendor firmware.
Check the conservative SRAM limit, flash mapping and live heap/stack headroom.
The custom target does not initialize Waveshare LCD pins or any other board's
antenna switch. No Wi-Fi/BLE stack may run concurrently.

## 2. Directed raw 1000-frame test

Use two C6 boards, initially without a display. Their default lab PAN is
`0xcafe`, channel 15, TX power 0 dBm. Node A uses short address `0x1234`, B
`0x5678`. Build A normally and reverse the address globals for B:

```sh
tinygo build -target ./targets/esp32c6-radio.json \
  -o build/raw-a.elf ./examples/raw-radio
tinygo build -target ./targets/esp32c6-radio.json \
  -ldflags="-X main.localAddress=0x5678 -X main.peerAddress=0x1234" \
  -o build/raw-b.elf ./examples/raw-radio
```

Start both within the receive-drain window. Each attempts **1000 accepted**
legacy unicast frames with an ACK request, an 8-bit DSN and a unique **16-bit**
payload sequence 0-999. The 16-bit ID avoids false duplicate/loss accounting
when the DSN wraps. Busy rejections retry only within an explicit admission
deadline; they are not counted as accepted or silently reported successful.
There are no automatic TX retries in this raw application.

For each direction reconcile:

- 1000 accepted = 1000 started = 1000 terminal callbacks.
- Terminal successes + no-ACK + channel-access failures + other/abort = 1000.
- Peer unique receives + missing payload IDs = 1000; report duplicates separately.
- Sniffer MAC frames, ACKs and DSNs agree with callbacks and actual loss.
- Record RX overflow/invalid/unsupported counters, not just application receives.

The example waits up to 30 seconds for remaining peer payloads, then closes the
radio and drains callbacks. A timeout is reported and missing receives remain
visible; it is not a pass. A lost ACK can coexist with a successful peer receive.
An error count is a real result, not a reason to synthesize a success callback.

Use a known-good 802.15.4 sniffer on channel 15 and capture pcapng with FCS
checking enabled. Confirm the application payload excludes FCS and that exactly
two FCS bytes appear over the air. Record the sniffer firmware/tool versions and
preserve the capture alongside both serial logs. Measure ACK turnaround with
the sniffer/logic analyzer under maximum RX load and a saturated callback queue.
Specifically exercise source-match hit/miss/disabled, promiscuous mode, CRC
errors, no peer, busy channel, close during TX, and disable/re-enable.

## 3. Always-on Thread end device

This gate cannot proceed safely until physical entropy and the
[durable storage gate](storage.md) pass. `examples/thread-node` supplies wiring,
not a ready-to-flash application or a pretend flash backend.

Use a separately known-good OpenThread router. A reproducible source option is
ESP-IDF v5.5's `examples/openthread/ot_cli`, pinned to
`8c750b088c7cd857d079c0eeb495da199b359461`; that SDK is for the **reference peer**,
not required by consumers of this module. Record its build config and `version`.
On a disposable lab router, create a fresh dataset rather than reuse a public
fixed key:

```text
dataset init new
dataset channel 15
dataset panid 0xcafe
dataset networkname c6-lab
dataset commit active
ifconfig up
thread start
state
dataset active -x
ipaddr
version
```

Import the complete lab dataset through the verified Settings-backed Instance,
not by writing raw dataset-only keys. The wiring example explicitly selects
RxOnWhenIdle, clears FullThreadDevice and disables router eligibility.
Confirm the role is child/end device and the router reports it as always-on.

Check real secured MLE/MAC exchange, then IPv6 echo in **both directions** using
the mesh-local addresses and the stack's ICMPv6 handlers. Capture the encrypted
wire traffic; host mock packets are not evidence. Reboot and physically
interrupt power during counter reservations/compaction; confirm dataset
restoration and strictly non-reused MAC/MLE security counters before accepting
recovery. Corrupt/missing counter history must stop secured operation.

## 4. Soak and Waveshare display

Run the proposed 30-minute attached soak, logging live heap, goroutine/queue
high-water marks, packet and overflow counters, parent state and resets.
Require bounded memory and reconcile traffic; do not infer a soak pass from
preallocated ISR storage alone.

Only after these gates pass, add an application-owned ST7789 status task and
repeat the directed test and soak. Waveshare pins: MOSI 6, SCLK 7, CS 14, DC 15,
RST 21, BL 22. Use at most 50% backlight brightness. Rendering and LCD SPI must
never run in the radio ISR, callback worker or Thread event loop. LCD support,
coexistence, router qualification, sleepy/CSL operation and certification are
not implemented by this module.
