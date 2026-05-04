/**
 * @file arch_trap.c
 * @brief RISC-V Trap/Exception Handler for Toob-Boot
 *
 * Minimal trap handler that dispatches hardware exceptions to the
 * Toob-Boot ECC/NMI bridge. In a bootloader context, we do not handle
 * interrupts — only synchronous exceptions (illegal instruction,
 * load/store access faults from Flash ECC errors).
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 0 (HardFault & ECC Guard)
 *    - "Der Handler MUSS existieren, das ECC_NMI Flag abfangen und zwingend
 *      das System asynchron über das Watchdog-Reset-Register neustarten."
 * 2. docs/concept_fusion.md
 *    - toob_ecc_trap() is the core-level bridge for NMI/HardFault events.
 * 3. RISC-V Privileged Spec v1.12
 *    - mcause exception codes: 5 = Load Access Fault, 7 = Store Access Fault.
 */

#include "arch_riscv.h"
#include "boot_hal.h"
#include <stdint.h>

/* mcause exception codes relevant for Flash/ECC faults */
#define MCAUSE_LOAD_ACCESS_FAULT   5U
#define MCAUSE_STORE_ACCESS_FAULT  7U
#define MCAUSE_ILLEGAL_INSTRUCTION 2U

/**
 * @brief Default RISC-V trap handler.
 *
 * Installed into mtvec during startup. On any synchronous exception,
 * reads mcause and dispatches to the appropriate handler.
 *
 * For load/store access faults (potential Flash ECC errors), we call
 * toob_ecc_trap() which sets the BOOT_ERR_ECC_HARDFAULT flag in
 * survival RAM and triggers a WDT reset.
 *
 * This function is marked __attribute__((interrupt)) to ensure the
 * compiler generates the correct mret-based return sequence and
 * saves/restores all caller-saved registers.
 */
void __attribute__((interrupt("machine"), aligned(4), section(".iram1.text")))
arch_riscv_trap_entry(void)
{
    uint32_t mcause = csr_read(mcause);

    /* Bit 31 distinguishes interrupts (1) from exceptions (0) */
    if ((mcause & (1U << 31)) != 0U) {
        /*
         * Asynchronous interrupt — should never fire in bootloader context
         * (interrupts are globally disabled). Infinite trap as safety net.
         */
        for (;;) {
            __asm__ volatile("wfi");
        }
    }

    uint32_t exception_code = mcause & 0x7FFFFFFFU;

    switch (exception_code) {
    case MCAUSE_LOAD_ACCESS_FAULT:
    case MCAUSE_STORE_ACCESS_FAULT:
        /*
         * Flash ECC or bus fault — delegate to Toob-Boot core.
         * toob_ecc_trap() will write BOOT_ERR_ECC_HARDFAULT to survival RAM
         * and force a WDT reset (never returns).
         */
        toob_ecc_trap();
        break;

    case MCAUSE_ILLEGAL_INSTRUCTION:
    default:
        /*
         * Unrecoverable. Enter an infinite loop.
         * The hardware WDT will reset the system.
         */
        break;
    }

    for (;;) {
        __asm__ volatile("wfi");
    }
}
