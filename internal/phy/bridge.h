// SPDX-License-Identifier: Apache-2.0
// Minimal ABI subset from the bundled ESP-IDF v5.5 headers. The originals
// remain in native/ for provenance; no ESP-IDF runtime or sdkconfig is used.
#pragma once

#include <stdbool.h>
#include <stdint.h>

typedef struct { uint8_t params[128]; } esp32c6_phy_init_data_t;
typedef struct {
    uint8_t version[4];
    uint8_t mac[6];
    uint8_t opaque[1894];
} esp32c6_phy_calibration_data_t;

int esp32c6_phy_calibrate(void);
void esp32c6_phy_close(void);
void esp32c6_baseband_init(void);
int esp32c6_phy_putchar(uint8_t value);
