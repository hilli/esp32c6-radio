# Third-party notices

Original project code is under the root BSD-3-Clause license, except files
explicitly marked Apache-2.0. Redistributed dependencies retain their original
notices; the native manifest pins every imported byte.

| Component | Exact revision | License and retained material |
|---|---|---|
| ESP-IDF v5.5 | `8c750b088c7cd857d079c0eeb495da199b359461` | Apache-2.0; original PHY public/private headers and C6 ROM PHY linker definitions in `internal/phy/native` |
| esp-phy-lib | `f6746ceea821b3800e8700d7e535f5df7304b8e5` | Apache-2.0; `libphy.a`, `libbtbb.a`, and original `LICENSE` |
| TinyGo v0.42.0 | `bdc4a21af007d8f059b5cefe5644b7ee7bda5b2d` | BSD-3-Clause; adapted C6 linker script, original `LICENSE-TINYGO` retained |
| gothread | `f61a13bebcdd740ca78807149f8677f30f19658f` | BSD-3-Clause; merged main revision, normal Go module dependency, not copied from the user's working tree |

`internal/phy/bridge.h` contains a minimal ABI subset of the pinned original
headers, not an ESP-IDF runtime. The C6 PHY parameter initializer and entropy
sampling procedure are adapted from that same ESP-IDF revision and marked
Apache-2.0. Generated weak ROM address declarations retain Espressif's notice.
`internal/phy/native/esp32c6-radio.ld` adapts TinyGo's linker layout and changes
the flash map, SRAM boundary, section coverage/alignment and stack reservation.

The Espressif archives are ordinary ar archives committed to Git and carried by
Go module zips and vendoring. They are not LFS pointers, separately downloaded
consumer blobs, or part of gothread. No `esp-coex-lib`, RF test library, Wi-Fi
binary, Bluetooth controller, or RTOS is bundled or linked.

Upstream source roots:

- <https://github.com/espressif/esp-idf/tree/8c750b088c7cd857d079c0eeb495da199b359461>
- <https://github.com/espressif/esp-phy-lib/tree/f6746ceea821b3800e8700d7e535f5df7304b8e5>
- <https://github.com/tinygo-org/tinygo/tree/bdc4a21af007d8f059b5cefe5644b7ee7bda5b2d>

Implementation research also consulted esp-hal at
`df66e5da92168ccb3fc582530edc5e42a6506e27` for PMU/modem and PHY entropy
prerequisites. No Rust runtime or Rust-built library is included.
