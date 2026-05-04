/**
 * @file arch_riscv.h
 * @brief RISC-V 32-bit Architecture Abstraction for Toob-Boot
 *
 * Provides CSR access macros, interrupt control, and the arch-level timing API.
 * Used by all RISC-V chips (ESP32-C3, ESP32-C6, GD32V, etc.).
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/structure_plan.md
 *    - hal/arch/riscv32/ contains ISA-level abstractions shared across vendors.
 * 2. docs/hals.md
 *    - clock_hal_t requires get_tick_ms() and delay_ms(), provided here via mtime.
 * 3. docs/concept_fusion.md
 *    - Bare-Metal register access. No SDK dependencies.
 * 4. RISC-V Privileged Spec v1.12
 *    - CSR encodings for mstatus, mcause, mtvec, mtime.
 */

#ifndef ARCH_RISCV_H
#define ARCH_RISCV_H

#include <stdint.h>

/* --- CSR Access Macros (GCC inline assembly) --- */

#define CSR_MSTATUS   0x300
#define CSR_MTVEC     0x305
#define CSR_MCAUSE    0x342
#define CSR_MTVAL     0x343
#define CSR_MIE       0x304

#define CSR_MSTATUS_MIE (1U << 3)

#define csr_read(csr)                                   \
    ({                                                  \
        uint32_t __val;                                 \
        __asm__ volatile("csrr %0, " #csr : "=r"(__val)); \
        __val;                                          \
    })

#define csr_write(csr, val)                             \
    __asm__ volatile("csrw " #csr ", %0" :: "r"((uint32_t)(val)))

#define csr_set(csr, val)                               \
    __asm__ volatile("csrs " #csr ", %0" :: "r"((uint32_t)(val)))

#define csr_clear(csr, val)                             \
    __asm__ volatile("csrc " #csr ", %0" :: "r"((uint32_t)(val)))

/* --- Interrupt Control --- */

static inline void arch_riscv_disable_interrupts(void)
{
    csr_clear(mstatus, CSR_MSTATUS_MIE);
}

static inline void arch_riscv_enable_interrupts(void)
{
    csr_set(mstatus, CSR_MSTATUS_MIE);
}

/* --- Timer API (chip-specific base address injected via chip_config.h) ---
 *
 * The ESP32-C6 CLINT mtime register is at a chip-specific base address.
 * These functions require CHIP_SYSTIMER_BASE and CHIP_CPU_FREQ_HZ to be
 * defined in chip_config.h before use.
 */

/**
 * @brief Initialize the architecture timer.
 * @param systimer_base  Base address of the SYSTIMER peripheral.
 * @param cpu_freq_hz    CPU clock frequency in Hz.
 */
void arch_riscv_timer_init(uint32_t systimer_base, uint32_t cpu_freq_hz);

/** @brief Returns monotonic milliseconds since timer init. */
uint32_t arch_riscv_get_tick_ms(void);

/** @brief Busy-wait delay. Caller must handle WDT kicks for long delays. */
void arch_riscv_delay_ms(uint32_t ms);

/* --- Trap Handler (defined in arch_trap.c, installed via mtvec) --- */

/** @brief Default trap vector. Reads mcause, dispatches to toob_ecc_trap(). */
void arch_riscv_trap_entry(void);

#endif /* ARCH_RISCV_H */
