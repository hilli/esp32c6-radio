/* Adapted from TinyGo v0.42.0 targets/esp32c6.ld (BSD-3-Clause).
 * ESP-IDF v5.5 components/esp_system/ld/esp32c6/memory.ld.in supplies
 * the conservative SRAM limit, excluding ROM and bootloader workspace.
 * Both instructions and constants use the same flash mapping: their
 * virtual offsets must equal their offsets in the ROM-loadable image.
 */
MEMORY
{
    SRAM (rwx) : ORIGIN = 0x40800000, LENGTH = 0x6e610
    FLASH (rx) : ORIGIN = 0x42000000, LENGTH = 4M
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
        /* Padding must be stored in .text, not an unencoded gap before rodata.
         * The ESP image has an 8-byte segment header between these sections.
         */
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

ASSERT(ADDR(.text) == ORIGIN(FLASH) + 0x18 +
       ((SIZEOF(.data) > 0) ? (SIZEOF(.data) + 8) : 0) +
       SIZEOF(.iram) + 16, "ESP image text offset does not match flash mapping");
ASSERT(ADDR(.rodata) == ADDR(.text) + SIZEOF(.text) + 8,
       "ESP image rodata offset does not match flash mapping");

/* ESP-IDF v5.5 esp32c6.rom.ld. */
PROVIDE(ets_delay_us = 0x40000040);
Cache_Invalidate_ICache_All = 0x4000064c;
Cache_Suspend_ICache = 0x40000698;
Cache_Resume_ICache = 0x4000069c;
Cache_MMU_Init = 0x400006b4;
Cache_MSPI_MMU_Set = 0x400006b8;
