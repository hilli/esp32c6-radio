// SPDX-License-Identifier: Apache-2.0
// ABI subset from ESP-IDF v5.5 esp_rom_spiflash.h.
#pragma once
#include <stdint.h>
typedef struct {
    uint32_t device_id, chip_size, block_size, sector_size, page_size, status_mask;
} c6_flash_chip;
int c6_flash_ready(void);
int c6_flash_mutate(uint32_t operation, uint32_t address, const uint32_t *buffer, uint32_t length);
uint32_t c6_flash_device_id(void);
int c6_shared_v2_flash_ready(void);
int c6_shared_v2_flash_mutate(uint32_t operation, uint32_t address, const uint32_t *buffer, uint32_t length);
