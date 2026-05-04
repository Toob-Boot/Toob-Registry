/**
 * @file vendor_reset_reason.c
 * @brief ESP Reset Reason Mapping (Vendor-Generic)
 *
 * Reads the chip-specific reset cause register and maps hardware codes to
 * the generic reset_reason_t enum. All chip-specific values (register address,
 * bit mask, code numbers) are injected via chip_config.h.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 5 (clock_hal_t)
 *    - get_reset_reason: "Abstrahiert das plattform-spezifische Reset-Register."
 *    - Must be cached on first read (idempotent).
 * 2. docs/concept_fusion.md Section 1.A
 *    - Priority: WDT > BROWNOUT > SOFTWARE > PIN > POWER_ON
 *
 * PORTABILITY:
 * - Register address:   REG_RESET_CAUSE      (chip_config.h)
 * - Bit mask:           REG_RESET_CAUSE_MASK  (chip_config.h)
 * - All CHIP_RST_* codes: chip_config.h
 */

#include "esp_common.h"
#include "chip_config.h"

#include <stdbool.h>
#include <stdint.h>

static bool s_cached;
static reset_reason_t s_reason;

reset_reason_t esp_get_reset_reason(void)
{
    if (s_cached) {
        return s_reason;
    }

    uint32_t raw = REG_READ(REG_RESET_CAUSE) & REG_RESET_CAUSE_MASK;

    switch (raw) {
    case CHIP_RST_TG0WDT:
    case CHIP_RST_TG1WDT:
    case CHIP_RST_RTCWDT_SYS:
    case CHIP_RST_TG0WDT_CPU:
    case CHIP_RST_RTCWDT_CPU:
    case CHIP_RST_RTCWDT_RTC:
#if CHIP_HAS_SWD
    case CHIP_RST_SWD:
#endif
        s_reason = RESET_REASON_WATCHDOG;
        break;

    case CHIP_RST_BROWNOUT:
        s_reason = RESET_REASON_BROWNOUT;
        break;

    case CHIP_RST_SW_SYS:
    case CHIP_RST_SW_CPU:
        s_reason = RESET_REASON_SOFTWARE;
        break;

    case CHIP_RST_POWERON:
    case CHIP_RST_DEEPSLEEP:
        s_reason = RESET_REASON_POWER_ON;
        break;

    default:
        s_reason = RESET_REASON_UNKNOWN;
        break;
    }

    s_cached = true;
    return s_reason;
}
