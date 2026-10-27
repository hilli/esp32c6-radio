//go:build tinygo && esp32c6 && esp32c6_radio

// SPDX-FileCopyrightText: 2015-2025 Espressif Systems (Shanghai) CO LTD
// SPDX-License-Identifier: Apache-2.0
#include "flash_bridge.h"
#include <stddef.h>

#define RAM __attribute__((section(".iram.c6flash"), noinline))
#define FIRST 0x3f8000u
#define LAST 0x400000u

extern int c6_rom_unlock(void);
extern int c6_rom_write(uint32_t, const uint32_t *, int32_t);
extern int c6_rom_erase(uint32_t);
extern int c6_rom_wait(c6_flash_chip *);
extern uint32_t c6_cache_suspend(void);
extern void c6_cache_resume(uint32_t);
extern void c6_cache_invalidate(void);
extern const uint8_t __settings_phys_start[], __settings_phys_end[];

static RAM int ram_range(uintptr_t p, uint32_t n) {
    return p >= 0x40800000u && p <= 0x40880000u-n;
}

RAM int c6_flash_ready(void) {
    if ((uintptr_t)__settings_phys_start != FIRST || (uintptr_t)__settings_phys_end != LAST) return -2;
    uintptr_t data = *(volatile uint32_t *)0x4087ffecu;
    uintptr_t funcs = *(volatile uint32_t *)0x4087fff0u;
    if (!ram_range(data, 28) || !ram_range(funcs, 52) ||
        *(volatile uint32_t *)0x4087fff4u != 0) return -2;
    volatile uint32_t *table=(volatile uint32_t *)funcs;
    volatile uint8_t *bits=(volatile uint8_t *)funcs;
    if (table[1]!=16 || table[2]!=32) return -2;
    for (unsigned i=0;i<4;i++) {
        if (bits[i]<23 || bits[i]>24) return -2;
    }
    // Reject installed SDK/custom callbacks, never replace them with no-ops.
    for (unsigned i=3; i<13; i++) {
        if (((volatile uint32_t *)funcs)[i] != 0) return -2;
    }
    c6_flash_chip *chip=(c6_flash_chip *)data;
    if (chip->chip_size < LAST || chip->sector_size != 4096 ||
        chip->page_size != 256 || chip->block_size != 65536 ||
        !(chip->status_mask&1) || (chip->device_id&255)!=0x16) return -2;
    return 0;
}

RAM uint32_t c6_flash_device_id(void) {
    if (c6_flash_ready()!=0) return 0;
    return ((c6_flash_chip *)(uintptr_t)*(volatile uint32_t *)0x4087ffecu)->device_id;
}

// All cache-off instructions/data/return paths are SRAM/ROM. No Go call,
// allocator, logger, jump table or SDK/RTOS callback is used.
RAM int c6_flash_mutate(uint32_t op,uint32_t address,const uint32_t *buffer,uint32_t length) {
    if (c6_flash_ready()!=0) return -2;
    if (op==1) {
        if (length==0 || length>256 || (length&3) || (address&3) ||
            address<FIRST || address>LAST-length ||
            (address&255)+length>256 ||
            !ram_range((uintptr_t)buffer,length) || ((uintptr_t)buffer&3)) return -1;
    } else if (op==2) {
        if ((address&4095) || address<FIRST || address>LAST-4096 || length!=4096) return -1;
    } else return -1;
    c6_flash_chip *chip=(c6_flash_chip *)(uintptr_t)*(volatile uint32_t *)0x4087ffecu;
    uintptr_t status;
    __asm__ volatile("csrrci %0, mstatus, 8" : "=r"(status) :: "memory");
    uint32_t autoload=c6_cache_suspend();
    int result=c6_rom_wait(chip);
    if (!result) result=c6_rom_unlock();
    if (!result) {
        if (op==1) result=c6_rom_write(address,buffer,(int32_t)length);
        else result=c6_rom_erase(address/4096);
    }
    // ROM WIP loops are unbounded on stuck/missing flash: fail-stop, NEVER
    // returned success or a jump into XIP while still busy. The independent
    // host campaign controller treats absent completion as FAIL/timeout.
    int idle=c6_rom_wait(chip);
    if (idle) { for (;;) __asm__ volatile("wfi"); }
    c6_cache_invalidate();
    c6_cache_resume(autoload);
    __asm__ volatile("fence.i" ::: "memory");
    if (status&8) __asm__ volatile("csrsi mstatus, 8" ::: "memory");
    return result;
}
