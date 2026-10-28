/* Experimental c6-shared-v2-lab1, adapted from the schema1 C6 target and
 * TinyGo v0.42.0 esp32c6.ld (BSD-3-Clause). This layout is mutually exclusive
 * with schema1; do not flash either layout over retained credentials.
 */
MEMORY
{
    SRAM (rwx) : ORIGIN = 0x40800000, LENGTH = 0x6e610
    FLASH (rx) : ORIGIN = 0x42000000, LENGTH = 0x3d0000
    SETTINGS (r) : ORIGIN = 0x423d0000, LENGTH = 0x30000
}

ENTRY(call_start_cpu0)
SECTIONS
{
    .stack (NOLOAD) :
    {
        . = ALIGN(16);
        . += _stack_size;
        _stack_top = .;
    } > SRAM
    .bss (NOLOAD) : ALIGN(4)
    {
        _sbss = ABSOLUTE(.);
        *(.sbss .sbss.*)
        *(.bss .bss.*)
        *(COMMON)
        . = ALIGN(4);
        _ebss = ABSOLUTE(.);
    } > SRAM
    .data : ALIGN(4)
    {
        _sdata = ABSOLUTE(.);
        *(.sdata .sdata.*)
        *(.data .data.*)
        *(.dram*)
        . = ALIGN(4);
        _edata = ABSOLUTE(.);
    } > SRAM
    .iram : ALIGN(4)
    {
        *(.init)
        *(.iram*)
        *(.text.handleInterruptASM)
        . = ALIGN(256);
        *(.text.exception_vectors)
        . = ALIGN(4);
    } > SRAM
    _heap_start = ALIGN(4);
    _heap_end = ORIGIN(SRAM) + LENGTH(SRAM);
    .irom_dummy (NOLOAD) :
    {
        . += 0x18;
        . += (SIZEOF(.data) > 0) ? (SIZEOF(.data) + 8) : 0;
        . += SIZEOF(.iram) + 8;
        . += 8;
    } > FLASH
    .text : ALIGN(4)
    {
        *(.text .text.*)
        . = ALIGN(. + 8, MAX(4, ALIGNOF(.rodata))) - 8;
    } > FLASH
    .rodata_dummy (NOLOAD) : { . += 8; } > FLASH
    .rodata : ALIGN(4)
    {
        *(.rodata .rodata.*)
        *(.srodata .srodata.*)
        . = ALIGN(4);
    } > FLASH
    /DISCARD/ : { *(.eh_frame) }
    .tinygo_stacksizes (INFO) : { *(.tinygo_stacksizes) }
    .tinygo_arm_entries (INFO) : { *(.tinygo_arm_entries) }
}
_globals_start = _sbss;
_globals_end = _edata;
_stack_size = 16K;
__settings_start = ORIGIN(SETTINGS);
__settings_end = ORIGIN(SETTINGS) + LENGTH(SETTINGS);
__settings_phys_start = 0x3d0000;
__settings_phys_end = 0x400000;
__c6_store_profile = 1;
__c6_store_schema = 2;
__c6_store_bank_bytes = 98304;
__c6_store_header_bytes = 128;
__c6_store_descriptor_bytes = 32;
__c6_store_directory_slots = 128;
__c6_store_arena_start = 4224;
__c6_store_retire_offset = 98300;
__c6_store_matter_bytes = 65536;
__c6_store_thread_bytes = 4096;

ASSERT(ADDR(.text) == ORIGIN(FLASH) + 0x18 +
       ((SIZEOF(.data) > 0) ? (SIZEOF(.data) + 8) : 0) +
       SIZEOF(.iram) + 16, "ESP image text offset does not match flash mapping");
ASSERT(ADDR(.rodata) == ADDR(.text) + SIZEOF(.text) + 8,
       "ESP image rodata offset does not match flash mapping");
ASSERT(ORIGIN(SETTINGS) - ORIGIN(FLASH) == 0x3d0000 &&
       LENGTH(SETTINGS) == 196608 &&
       ORIGIN(SETTINGS) + LENGTH(SETTINGS) == 0x42400000,
       "invalid c6-shared-v2-lab1 partition");

/* ESP-IDF v5.5 esp32c6.rom.ld; keep absolute ROM bindings in the linker. */
PROVIDE(ets_delay_us = 0x40000040);
Cache_Invalidate_ICache_All = 0x4000064c;
Cache_Suspend_ICache = 0x40000698;
Cache_Resume_ICache = 0x4000069c;
Cache_MMU_Init = 0x400006b4;
Cache_MSPI_MMU_Set = 0x400006b8;
PROVIDE(c6_rom_unlock = 0x40000154);
PROVIDE(c6_rom_write = 0x4000014c);
PROVIDE(c6_rom_erase = 0x40000144);
PROVIDE(c6_rom_wait = 0x40000110);
PROVIDE(c6_cache_suspend = 0x40000698);
PROVIDE(c6_cache_resume = 0x4000069c);
PROVIDE(c6_cache_invalidate = 0x4000064c);
