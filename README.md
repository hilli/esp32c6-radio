# esp32c6-radio

Experimental ESP32-C6 IEEE 802.15.4 support for TinyGo and
[`gothread`](https://github.com/hilli/gothread). Implements `radio.Radio` with
bundled, pinned Espressif PHY/baseband archives. No ESP-IDF installation,
submodules, Git LFS, `go generate`, or manual blob downloads are needed.

**Software-validated, not hardware-ready.** No board has been used to verify
PHY calibration, interrupts, RF, entropy quality, attachment, or reboot recovery.
The [Thread wiring example](examples/thread-node) deliberately requires verified
durable Settings; TinyGo 0.42.0 has no C6 flash driver. There is no unsafe
RAM-settings or resettable-frame-counter fallback.

## Toolchain and target

Validated with **Go 1.27.3 and TinyGo 0.42.0**. `mise.toml` pins TinyGo:

```sh
mise install
mise exec -- make check
mise exec -- make consumer
```

Or install those toolchains normally and run `make check`. The module's Go
language baseline is 1.26.

Use the supplied target, **not bare `esp32c6` or unmodified `xiao-esp32c6`**:

```sh
tinygo build -target ./targets/esp32c6-radio.json \
  -o build/bringup.elf ./examples/bringup
tinygo build -target ./targets/esp32c6-radio.json \
  -o build/raw-radio.bin ./examples/raw-radio
```

It inherits the concrete XIAO target to obtain TinyGo's required machine pin
definitions, but does not configure XIAO board peripherals or antenna switches.
Radio code does not use board GPIOs; do not assume the inherited I2C/SPI aliases
match Waveshare wiring. The custom linker uses a single identity
flash mapping and excludes ROM/bootloader SRAM workspace. Assumptions: C6,
40 MHz crystal, TinyGo's fixed 160 MHz CPU / 40 MHz APB, and 4 MiB flash.
Applications on other C6 boards own their board pins and RF switches.
Do not concurrently reconfigure the modem/PHY or its ADC entropy source.

For a separate application, download the module normally:

```sh
go get github.com/hilli/esp32c6-radio@main
```

The application's target JSON can be:

```json
{
  "inherits": ["xiao-esp32c6"],
  "build-tags": ["esp32c6_radio"],
  "linkerscript": "esp32c6-radio.ld"
}
```

TinyGo finds the bundled linker script through the driver's native library
search path, including in `go mod vendor` builds. No SDK-relative paths are used.
The module takes CPU interrupt **11**; TinyGo uses 6/7/9/10 for GPIO/UART/timer/USB.
Do not assign another application interrupt to line 11.

## Driver contract

```go
driver, err := esp32c6radio.New()
if err != nil { panic(err) } // unsupported targets return ErrUnsupported
if err := driver.SetCallbacks(callbacks, nil); err != nil { panic(err) }
if err := driver.Enable(); err != nil { panic(err) }
if err := driver.Receive(15); err != nil { panic(err) }
```

Check every returned error. `New` does not start RF; `Enable` calibrates and tests
a real MAC timer interrupt. For Thread, enable before constructing the Instance
so the PHY-backed `Entropy.Fill` is available, then bind
`driver.SetCallbacks(inst.SubMac(), inst.PostTask)` before receiving.

- Channels 11-26; PSDUs **exclude FCS**, contain 3-125 bytes, and must be legacy
  2003/2006 frames. Hardware length includes the two FCS bytes. RX RSSI/LQI
  replace the hardware buffer's FCS bytes and are not passed as payload.
- One accepted TX at a time; ordered attempt-start and exactly one terminal
  callback, including CCA failure, no ACK, abort, and shutdown. CSMA backoff and
  retries remain in SubMac; the radio performs physical CCA when requested.
- Advertised capabilities: ACK timeout and hardware maximum-energy scanning.
  Scan duration is 1-16777 ms. No autonomous retries, CSMA backoff, timed TX/RX,
  CSL, enhanced ACKs, or hardware encryption are advertised.
- Inbound legacy ACK turnaround is hardware-managed. Source matching uses
  bounded 16-short / 16-extended address tables and updates the pending bit in
  the RX ISR, never in the worker. Promiscuous mode does not acknowledge traffic.
  Configuration changes wait for an in-progress RX/automatic ACK to finish.
- TX powers are representable values: -15 through +18 dBm in 3 dB steps, plus
  +20 dBm. Other values fail rather than being silently rounded. Sensitivity
  follows ESP-IDF v5.5's -120 dBm OpenThread baseline, **not a board measurement**.
- `GetRSSI` is the latest received/energy-scan sample, or `RSSIInvalid`, not an
  instantaneous noise measurement. Timestamps are monotonic microseconds at
  SFD interrupt observation, not calibrated over-the-air timestamps.
- Received/callback frames are borrowed **until callback return**. Copy to
  retain them. TX input is copied into fixed, word-aligned, stable DMA storage;
  queued callbacks have their own payload/metadata storage.
- The 16-slot event ring reserves completion slots before accepting TX/scan.
  RX overflow is counted in `Statistics` and coalesced into
  `HandleReceiveDone(nil, ErrOverflow)`. Invalid/unsupported RX frames are counted.
  The ISR never calls user callbacks or `PostTask`, allocates, or waits.
  A hardware ACK can precede a later queue drop; TX success is not proof of
  application delivery.
- `Sleep` stops the MAC receiver but retains the PHY for entropy and fast reuse;
  low-power/deep-sleep operation is not implemented. Void hardware-security
  methods count unsupported calls; gothread must encrypt in software.

The event worker may block in `PostTask`, which is why shutdown order matters:

```go
if err := driver.Close(); err != nil {
    panic(err) // unproven hardware quiescence requires a board reset
}
<-driver.Done() // outside callbacks/the Instance loop, with that loop still alive
inst.Stop()
```

`Close` is nonblocking with respect to callback draining and is safe inside a
callback. It stops producers and aborts accepted TX. `Done` closes after the last
callback returns. `Disable`/`Enable` is reversible; `Close` is terminal. Do not
wait on `Done` from a callback or stop the Instance before draining.

## Examples and remaining gates

`examples/bringup` exercises native calibration/baseband init and a MAC-timer
IRQ probe, then reports timer uptime. It sends no frames.

`examples/raw-radio` sends 1000 directed, unsecured lab frames with ACK requests,
unique 16-bit payload IDs and reconciled counters. A second board reverses
`main.localAddress` and `main.peerAddress` via TinyGo `-ldflags`. See the exact
[physical acceptance procedure](docs/hardware-testing.md).

`examples/thread-node` is an application-wiring package, **not a standalone
flashable image**. It accepts a complete preconfigured dataset and a verified
durable `platform.Settings`, restores through the Instance, checks persistence
errors, selects an always-on minimal end device, and serializes startup/shutdown
on `PostTask`. `tools/thread-link` is an explicitly non-operational link check,
with failing storage, **never firmware to flash**.

The [storage investigation](docs/storage.md) records the source-supported C6 ROM
route and the unclosed cache/flash/power-loss gates. Until those and physical
entropy validation pass, do not provision persistent Thread credentials.
Encrypted traffic, bidirectional IPv6 echo, reboot recovery and the proposed
30-minute bounded-memory soak remain unverified.

Waveshare ESP32-C6-LCD-1.47 uses a C6FH4, 4 MiB flash and ST7789 172x320 display.
LCD work is deferred until radio/Thread hardware gates pass. Application-only
pins are MOSI 6, SCLK 7, CS 14, DC 15, RST 21 and BL 22; brightness must stay at
or below the manufacturer's 50% limit. No display driver runs in the radio or ISR.

## Native provenance and checks

The minimum native closure is `libphy.a` plus `libbtbb.a` from the exact
`esp-phy-lib` submodule revision used by ESP-IDF v5.5. Real adapters implement
formatted serial diagnostics and the 40 MHz crystal query; ROM bindings are weak
fallbacks so library patches win. No FreeRTOS, Wi-Fi/BLE stack, or coexistence
library/stubs are linked.

See [third-party notices](THIRD_PARTY.md) and the committed
[SHA256/source manifest](internal/phy/native/manifest.json).
Maintainers can verify offline with `go run ./tools/import-native`; `-fetch`
reimports those exact checksummed upstream bytes and regenerates the ROM
bindings. Consumers never need either command.

`make check` runs lint, host build, race tests, firmware builds, and exact ELF/bin
layout checks. `make consumer` checks cold Go caches and vendoring against the
local module. After publishing, test normal module downloads with:

```sh
go run ./tools/check-consumer -version=<published-commit>
```

The checker verifies native bytes/licenses and both directly downloaded and
vendored TinyGo images. Host tests and link success do not establish RF behavior.
