/**
 * @file vendor_rtc_mem.c
 * @brief ESP Confirm HAL via LP_RAM (Low-Power SRAM)
 *
 * Implements confirm_hal_t using the ESP32-C6 LP_RAM (RTC-resilient SRAM)
 * at 0x50000000. This memory survives power-on resets and deep-sleep cycles,
 * making it ideal for the 2FA boot nonce handoff.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 2 (confirm_hal_t)
 *    - check_ok: "MUSS die abgelegte Nonce bei positivem Match zwingend auf 0
 *      ueberschreiben, um Replay-Angriffe zu verhindern."
 *    - clear: "Ueberschreibt mit 0, Read-After-Write Verify (GAP-C08)."
 * 2. docs/concept_fusion.md Section 1.A (Stage 1 Confirm)
 *    - "Kryptografische 64-Bit Boot-Nonce (verhindert Brute-Force gegen
 *      RTC-RAM Wraparounds)."
 * 3. aggregated_scan.json metadata
 *    - LP_RAM boundary verified at 0x50000000 (8KB).
 */

#include "esp_common.h"
#include "chip_config.h"

#include <stdbool.h>
#include <stdint.h>

boot_status_t esp_confirm_init(void)
{
    /* LP_RAM is always accessible after reset — no init required. */
    return BOOT_OK;
}

void esp_confirm_deinit(void)
{
    /* LP_RAM persists — no teardown needed. */
}

/**
 * @brief Check if the OS confirmed the boot via nonce match.
 *
 * Reads the 64-bit nonce from LP_RAM and compares against the expected value.
 * On match: zeroes the nonce immediately to prevent replay attacks, then
 * returns true. On mismatch: returns false without modifying memory.
 *
 * Constant-time comparison is NOT required here — the nonce is not a secret,
 * it is a freshness token. Timing side-channels are irrelevant.
 */
bool esp_confirm_check_ok(uint64_t expected_nonce)
{
    volatile uint64_t *nonce_ptr = ADDR_CONFIRM_RTC_RAM;
    uint64_t stored = *nonce_ptr;

    if (stored != expected_nonce) {
        return false;
    }

    /* Zeroize immediately (anti-replay per hals.md) */
    *nonce_ptr = 0ULL;

    /* Read-after-write verify (GAP-C08) */
    uint64_t verify = *nonce_ptr;
    if (verify != 0ULL) {
        return false;
    }

    return true;
}

/**
 * @brief Clear the confirm nonce from LP_RAM.
 *
 * Writes zero and verifies via read-after-write. Used during rollback
 * or explicit state invalidation.
 */
boot_status_t esp_confirm_clear(void)
{
    volatile uint64_t *nonce_ptr = ADDR_CONFIRM_RTC_RAM;
    *nonce_ptr = 0ULL;

    /* Read-after-write verify (GAP-C08) */
    uint64_t verify = *nonce_ptr;
    if (verify != 0ULL) {
        return BOOT_ERR_STATE;
    }

    return BOOT_OK;
}
