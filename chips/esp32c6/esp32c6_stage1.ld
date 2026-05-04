/* ===========================================================================
 * ESP32-C6 Stage 1 Linker Script
 *
 * Memory map derived from:
 *   1. archive/toobfuzzer3/blueprints/esp32-c6/run_33/blueprint.json
 *   2. hal/chips/esp32c6/chip_config.h (IRAM/LP_RAM regions)
 *
 * IRAM layout (0x40800000, 512KB total):
 *   - BootROM reserved tail: last 16KB (0x4087C000-0x4087FFFF)
 *   - Usable IRAM: 496KB (0x40800000-0x4087BFFF)
 *
 * LP_RAM (0x50000000, 16KB):
 *   - Used for confirm_hal (vendor_rtc_mem.c) and .noinit handoff state.
 * =========================================================================== */

INCLUDE generated_memory.ld

ENTRY(_start)

MEMORY
{
    /* Instruction RAM — code executes from here (XIP from flash is also
     * possible via 0x42000000, but the bootloader runs entirely from IRAM
     * for resilience during flash operations). */
    iram (rwx)  : ORIGIN = 0x40800000, LENGTH = 0x7C000  /* 496 KB */

    /* LP_RAM survives all resets except full power loss.
     * Used for .noinit section (toob_handoff_state, toob_diag_state). */
    lp_ram (rw) : ORIGIN = 0x50000000, LENGTH = 0x4000   /* 16 KB */
}

SECTIONS
{
    /* --- Entry point (placed first for BootROM handoff) --- */
    .entry : ALIGN(16)
    {
        KEEP(*(.entry))
    } > iram

    /* --- IRAM-resident code (must survive SPI cache disable) --- */
    .iram1.text : ALIGN(4)
    {
        *(.iram1.text)
        *(.iram1.text.*)
    } > iram

    /* --- Main code section --- */
    .text : ALIGN(16)
    {
        _text_start = .;
        *(.vectors)
        *(.literal.entry)
        *(.text.entry)
        *(.literal .literal.*)
        *(.text .text.*)
        _text_end = .;
    } > iram

    /* --- Read-only and initialized data --- */
    .data : ALIGN(16)
    {
        _data_start = .;
        __global_pointer$ = . + 0x800;  /* RISC-V GP register */
        *(.data .data.*)
        *(.sdata .sdata.*)
        *(.rodata .rodata.*)
        *(.gnu.linkonce.r.*)
        _data_end = .;
    } > iram

    /* --- Uninitialized data (zeroed by startup BSS init) --- */
    .bss (NOLOAD) : ALIGN(16)
    {
        _bss_start = .;
        *(.bss .bss.*)
        *(.sbss .sbss.*)
        *(COMMON)
        _bss_end = .;
    } > iram

    /* --- .noinit section: survives resets, placed in LP_RAM ---
     * Contains toob_handoff_state and toob_diag_state for OS handoff.
     * NOT zeroed by BSS init — values persist across warm resets. */
    .noinit (NOLOAD) : ALIGN(8)
    {
        *(.noinit)
        *(.noinit.*)
    } > lp_ram

    /* Stack grows downward from end of IRAM */
    _stack_top = ORIGIN(iram) + LENGTH(iram);
}
