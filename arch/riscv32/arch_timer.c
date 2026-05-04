/**
 * @file arch_timer.c
 * @brief RISC-V mtime/SYSTIMER based timing for Toob-Boot
 *
 * Provides millisecond-resolution timing via the hardware SYSTIMER peripheral.
 * On ESP32-C6, the SYSTIMER runs at 16 MHz (independent of CPU frequency),
 * providing a stable timebase even during clock changes.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md
 *    - clock_hal_t.get_tick_ms(): Monotonic ms since boot.
 *    - clock_hal_t.delay_ms(): Blocking wait. WDT kicks are caller's duty.
 * 2. ESP32-C6 TRM Section 11 (SYSTIMER)
 *    - SYSTIMER_UNIT0_VALUE registers provide a 52-bit counter at 16 MHz.
 *    - SYSTIMER_UNIT0_OP (bit 30 = update request) triggers latch.
 */

#include "arch_riscv.h"
#include <stdint.h>

/* SYSTIMER register offsets (relative to SYSTIMER base) */
#define SYSTIMER_UNIT0_OP          0x0004
#define SYSTIMER_UNIT0_VALUE_LO    0x000C
#define SYSTIMER_UNIT0_VALUE_HI    0x0010

/* SYSTIMER runs at a fixed 16 MHz on ESP32-C6, regardless of CPU clock */
#define SYSTIMER_TICKS_PER_MS      16000U

static volatile uint32_t *s_systimer_base;

static inline uint32_t reg_read(uint32_t offset)
{
    return *(volatile uint32_t *)((uintptr_t)s_systimer_base + offset);
}

static inline void reg_write(uint32_t offset, uint32_t val)
{
    *(volatile uint32_t *)((uintptr_t)s_systimer_base + offset) = val;
}

/**
 * @brief Read the 52-bit SYSTIMER value as a 64-bit count.
 *
 * Triggers a latch-update via UNIT0_OP, then reads hi/lo atomically.
 * The latch ensures the hi+lo pair is consistent (no tearing).
 */
static uint64_t systimer_read_count(void)
{
    /* Request counter latch: set bit 30 of UNIT0_OP */
    reg_write(SYSTIMER_UNIT0_OP, 1U << 30);

    /*
     * Spin until the hardware acknowledges the latch.
     * Bit 29 is cleared by HW when the snapshot is ready.
     * Bounded loop for P10 compliance (max 1000 iterations).
     */
    for (uint32_t guard = 0; guard < 1000U; guard++) {
        if ((reg_read(SYSTIMER_UNIT0_OP) & (1U << 29)) == 0U) {
            break;
        }
    }

    uint32_t lo = reg_read(SYSTIMER_UNIT0_VALUE_LO);
    uint32_t hi = reg_read(SYSTIMER_UNIT0_VALUE_HI);

    return ((uint64_t)(hi & 0xFFFFFU) << 32) | (uint64_t)lo;
}

void arch_riscv_timer_init(uint32_t systimer_base, uint32_t cpu_freq_hz)
{
    (void)cpu_freq_hz; /* SYSTIMER is fixed at 16 MHz, CPU freq not needed */
    s_systimer_base = (volatile uint32_t *)(uintptr_t)systimer_base;
}

uint32_t arch_riscv_get_tick_ms(void)
{
    uint64_t ticks = systimer_read_count();
    return (uint32_t)(ticks / SYSTIMER_TICKS_PER_MS);
}

void arch_riscv_delay_ms(uint32_t ms)
{
    uint64_t target = systimer_read_count() + ((uint64_t)ms * SYSTIMER_TICKS_PER_MS);

    while (systimer_read_count() < target) {
        /* Busy-wait. WDT kicks are the caller's responsibility (hals.md). */
    }
}
