//go:build tinygo && esp32c6

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2016-2024 Espressif Systems (Shanghai) CO LTD
#include "bridge.h"
#include <stdarg.h>
#include <stdio.h>

extern int register_chipv7_phy(const esp32c6_phy_init_data_t *,
                              esp32c6_phy_calibration_data_t *, int);
extern void phy_init_param_set(uint8_t);
extern void phy_bbpll_en_usb(bool);
extern void phy_close_rf(void);
extern void phy_xpd_tsens(void);
extern void bt_bb_v2_init_cmplx(uint8_t);
extern void bt_bb_set_zb_tx_on_delay(uint16_t);

_Static_assert(sizeof(esp32c6_phy_calibration_data_t) == 1904, "calibration ABI");
_Static_assert(sizeof(esp32c6_phy_init_data_t) == 128, "init ABI");

// ESP-IDF v5.5 esp32c6/phy_init_data.c with MAX_TX_POWER=20.
static const esp32c6_phy_init_data_t init_data __attribute__((aligned(4))) = {{
    [0] = 0x0a, [2] = 80, [3] = 80, [4] = 80, [5] = 80, [6] = 80,
    [7] = 76, [8] = 76, [9] = 76, [10] = 76, [11] = 72, [12] = 68,
    [13] = 60, [14] = 60, [15] = 60, [16] = 76, [17] = 76, [18] = 76,
    [19] = 72, [127] = 0x51
}};
static esp32c6_phy_calibration_data_t calibration __attribute__((aligned(4)));

int esp32c6_phy_calibrate(void) {
    phy_init_param_set(1);
    phy_bbpll_en_usb(true);
    return register_chipv7_phy(&init_data, &calibration, 2); // PHY_RF_CAL_FULL
}

void esp32c6_baseband_init(void) {
    bt_bb_v2_init_cmplx(1);
    bt_bb_set_zb_tx_on_delay(50);
}

void esp32c6_phy_close(void) {
    phy_close_rf();
    phy_xpd_tsens();
}

int rtc_clk_xtal_freq_get(void) {
    return 40; // The supported C6 targets use a 40 MHz crystal.
}

int phy_printf(const char *format, ...) {
    char buffer[256];
    va_list args;
    va_start(args, format);
    int result = vsnprintf(buffer, sizeof(buffer), format, args);
    va_end(args);
    if (result < 0) {
        return result;
    }
    unsigned n = (unsigned)result;
    if (n >= sizeof(buffer)) {
        n = sizeof(buffer) - 1;
    }
    for (unsigned i = 0; i < n; i++) {
        if (esp32c6_phy_putchar((uint8_t)buffer[i]) != 0) {
            return -1;
        }
        if ((unsigned)result >= sizeof(buffer)) {
            static const char warning[] = "[PHY log truncated]\n";
            for (unsigned i = 0; i < sizeof(warning)-1; i++) {
                if (esp32c6_phy_putchar((uint8_t)warning[i]) != 0) {
                    return -1;
                }
            }
            return -1;
        }
    }
    return result;
}
