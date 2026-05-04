/**
 * @file startup.c
 * @brief ESP32-C6 Early Hardware Initialization (Pre-C Runtime)
 *
 * This file provides the absolute first code that runs after the BootROM
 * hands off to our binary. It must:
 * 1. Set up the stack pointer
 * 2. Clear BSS
 * 3. Sterilize ALL watchdogs (prevent resets during init)
 * 4. Install the trap vector
 * 5. Initialize HAL platform, run boot_main(), jump to OS
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/concept_fusion.md Section 1.B Layer 1
 *    - "startup.c Software-Sperre fungiert nur als Defense-in-Depth Netz."
 *    - "hal_deinit() vor dem Jump zum Feature-OS."
 * 2. docs/hals.md Section 0 (HardFault & ECC Guard)
 *    - "MUSS einen asynchronen HardFault_Handler definieren."
 * 3. blueprints/quirks/esp32-c6.json
 *    - WDT kill sequence: Unlock(0x50D83AA1) -> Config=0 -> Lock(0)
 */

#include "chip_config.h"
#include "boot_config_mock.h"
#include "arch_riscv.h"
#include "esp_common.h"
#include "boot_main.h"

#include <stdint.h>

/* Linker-provided symbols */
extern uint32_t _bss_start;
extern uint32_t _bss_end;
extern uint32_t _stack_top;

/* Forward declarations */
extern const boot_platform_t *boot_platform_init(void);
extern void arch_riscv_trap_entry(void);

/**
 * @brief Sterilize all watchdogs immediately after reset.
 *
 * Must run BEFORE any C library init, since the BootROM may leave
 * watchdogs armed with very short timeouts.
 *
 * Sequence per watchdog: Unlock -> Write 0 to config -> Re-lock.
 */
static void wdt_sterilize_all(void)
{
    /* TIMG0 WDT */
    REG_WRITE(REG_TIMG0_WDT_WPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(REG_TIMG0_WDT_CONFIG0, 0U);
    REG_WRITE(REG_TIMG0_WDT_WPROTECT, 0U);

    /* TIMG1 WDT */
    REG_WRITE(REG_TIMG1_WDT_WPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(REG_TIMG1_WDT_CONFIG0, 0U);
    REG_WRITE(REG_TIMG1_WDT_WPROTECT, 0U);

    /* LP_WDT (RTC WDT) */
    REG_WRITE(REG_LP_WDT_WPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(REG_LP_WDT_CONFIG0, 0U);
    REG_WRITE(REG_LP_WDT_WPROTECT, 0U);

#if CHIP_HAS_SWD
    /* SWD (Super Watchdog): Unlock -> set auto-feed bit (bit 30) -> re-lock */
    REG_WRITE(REG_LP_WDT_SWD_WPROTECT, VAL_WDT_UNLOCK);
    REG_SET_BIT(REG_LP_WDT_SWD_CONFIG, (1U << 30));
    REG_WRITE(REG_LP_WDT_SWD_WPROTECT, 0U);
#endif
}

/**
 * @brief Clear BSS segment.
 *
 * Zeroes all uninitialized static/global data. Required before any
 * C code that relies on zero-initialized globals.
 */
static void bss_init(void)
{
    uint32_t *p = &_bss_start;
    while (p < &_bss_end) {
        *p = 0U;
        p++;
    }
}

/**
 * @brief Jump to the OS entry point via RISC-V indirect jump.
 *
 * Uses jalr x0 (pseudo: jr) to perform a non-returning branch to the
 * application firmware vector table. The address MUST be 4-byte aligned
 * (verified by boot_main bounds check).
 */
static void __attribute__((noreturn))
jump_to_os(uint32_t entry_point)
{
    __asm__ volatile(
        "jr %0"
        :
        : "r"(entry_point)
    );
    __builtin_unreachable();
}

void _start(void);

/**
 * @brief Entry point from the BootROM / linker.
 *
 * This function is placed at the entry point address defined in the
 * linker script. It sets up the minimal C runtime environment,
 * initializes the HAL platform, runs boot_main(), and jumps to the OS.
 */
void __attribute__((noreturn, section(".entry")))
_start(void)
{
    /*
     * Step 1: Sterilize watchdogs FIRST — before any other code.
     * The BootROM may leave them armed with aggressive timeouts.
     */
    wdt_sterilize_all();

    /* Step 2: Install the RISC-V trap handler (mtvec) */
    csr_write(mtvec, (uint32_t)(uintptr_t)&arch_riscv_trap_entry);

    /* Step 3: Disable interrupts globally (bootloader runs uninterrupted) */
    arch_riscv_disable_interrupts();

    /* Step 4: Clear BSS */
    bss_init();

    /* Step 5: Initialize the HAL platform (clock, flash, wdt, crypto, ...) */
    const boot_platform_t *platform = boot_platform_init();

    /* Step 6: Run the boot state machine */
    static boot_target_config_t target;
    boot_status_t result = boot_main(platform, &target);

    /* Step 7: On success, jump to the resolved OS entry point */
    if (result == BOOT_OK && target.active_entry_point != 0U) {
        jump_to_os(target.active_entry_point);
    }

    /* Step 8: Unreachable — boot_main panics on failure internally.
     * If we somehow get here, spin for WDT reset. */
    for (;;) {
        __asm__ volatile("wfi");
    }
}
