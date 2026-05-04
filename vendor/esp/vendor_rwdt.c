/**
 * @file vendor_rwdt.c
 * @brief ESP Watchdog HAL via Direct Register Writes
 *
 * Implements wdt_hal_t using the TIMG0 RWDT (Regular Watchdog Timer).
 * Additionally sterilizes TIMG1 and LP_WDT/SWD to prevent spurious resets.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 3 (wdt_hal_t)
 *    - init: "Vendor Ports MUESSEN den TIMING_SAFETY_FACTOR bei der internen
 *      Register-Allokation auf timeout_ms_required beaufschlagen."
 *    - suspend/resume: "Safe Prescaler Injection for blocking Erase-ROM Functions."
 * 2. docs/concept_fusion.md Section 1.A
 *    - WDT used for auto-rollback on unconfirmed boot.
 * 3. chip_config.h
 *    - All register addresses and unlock keys are chip-specific constants.
 *    - CHIP_HAS_SWD guards Super Watchdog logic (C3/C6/H2 only).
 */

#include "esp_common.h"
#include "chip_config.h"

#include <stdint.h>

/* TIMG0 WDT register aliases (absolute addresses from chip_config.h) */
#define TIMG0_WDTCONFIG0     REG_TIMG0_WDT_CONFIG0
#define TIMG0_WDTCONFIG1     (REG_TIMG0_WDT_CONFIG0 + 0x04U)
#define TIMG0_WDTCONFIG2     (REG_TIMG0_WDT_CONFIG0 + 0x08U)
#define TIMG0_WDTCONFIG3     (REG_TIMG0_WDT_CONFIG0 + 0x0CU)
#define TIMG0_WDTFEED        REG_TIMG0_WDT_FEED
#define TIMG0_WDTWPROTECT    REG_TIMG0_WDT_WPROTECT

/* TIMG1 WDT (same layout, different base — addresses from chip_config.h) */
#define TIMG1_WDTCONFIG0     REG_TIMG1_WDT_CONFIG0
#define TIMG1_WDTWPROTECT    REG_TIMG1_WDT_WPROTECT

/* LP_WDT */
#define LPWDT_CONFIG0        REG_LP_WDT_CONFIG0
#define LPWDT_WPROTECT       REG_LP_WDT_WPROTECT

/* SWD (Super Watchdog) */
#define SWD_CONFIG           REG_LP_WDT_SWD_CONFIG
#define SWD_WPROTECT         REG_LP_WDT_SWD_WPROTECT

/* Config0 bit fields for TIMG RWDT */
#define WDT_EN_BIT           (1U << 31)
#define WDT_FLASHBOOT_EN_BIT (1U << 14)

/* SWD disable: Bit 30 = swd_auto_feed_en */
#define SWD_AUTO_FEED_BIT    (1U << 30)

static uint32_t s_saved_prescaler;

/**
 * @brief Disable a single WDT peripheral (Unlock → Config=0 pattern).
 *
 * All ESP WDTs are write-protected. The unlock key value is chip-specific
 * and provided via VAL_WDT_UNLOCK in chip_config.h.
 */
static void wdt_disable(uint32_t config_addr, uint32_t wprotect_addr)
{
    REG_WRITE(wprotect_addr, VAL_WDT_UNLOCK);
    REG_WRITE(config_addr, 0U);
    REG_WRITE(wprotect_addr, 0U);
}

/**
 * @brief Disable the SWD (Super Watchdog).
 *
 * The SWD requires its own unlock register and uses a different disable
 * mechanism (bit 30 = auto-feed enable).
 */
static void swd_disable(void)
{
    REG_WRITE(SWD_WPROTECT, VAL_WDT_UNLOCK);
    REG_SET_BIT(SWD_CONFIG, SWD_AUTO_FEED_BIT);
    REG_WRITE(SWD_WPROTECT, 0U);
}

boot_status_t esp_rwdt_init(uint32_t timeout_ms)
{
    /*
     * Step 1: Sterilize ALL watchdogs first (prevent reset during config).
     * This is safe because boot_platform_init() calls us early.
     */
    wdt_disable(TIMG1_WDTCONFIG0, TIMG1_WDTWPROTECT);
    wdt_disable(LPWDT_CONFIG0, LPWDT_WPROTECT);

#if CHIP_HAS_SWD
    swd_disable();
#endif

    /*
     * Step 2: Configure TIMG0 RWDT as the single boot watchdog.
     *
     * TIMG0 RWDT prescaler formula (ESP32-C6 TRM Section 10.4):
     *   tick_period = prescaler / APB_CLK_FREQ
     *   timeout_ticks = timeout_ms * APB_CLK_FREQ / (prescaler * 1000)
     *
     * With default APB_CLK = 40MHz and prescaler = 40000:
     *   1 tick = 1ms, so timeout_ticks = timeout_ms directly.
     *
     * TIMING_SAFETY_FACTOR (hals.md): We apply a 2x margin.
     */
    uint32_t effective_timeout = timeout_ms * 2U;

    /* Prescaler: 40000 → 1 WDT tick = 1ms at 40MHz APB */
    uint32_t prescaler = 40000U;
    s_saved_prescaler = prescaler;

    REG_WRITE(TIMG0_WDTWPROTECT, VAL_WDT_UNLOCK);

    /* CONFIG1: prescaler value (bits 15:0) */
    REG_WRITE(TIMG0_WDTCONFIG1, prescaler << 16);

    /* CONFIG2: Stage 0 timeout (system reset after this many ticks) */
    REG_WRITE(TIMG0_WDTCONFIG2, effective_timeout);

    /* CONFIG3: Stage 1 timeout (interrupt, not used — set to max) */
    REG_WRITE(TIMG0_WDTCONFIG3, 0xFFFFFFFFU);

    /* CONFIG0: Enable WDT, Stage 0 action = system reset (0x3) */
    uint32_t config0 = WDT_EN_BIT
                     | (0x3U << 29)  /* STG0 action: system reset */
                     | (0x0U << 27); /* STG1 action: off */
    REG_WRITE(TIMG0_WDTCONFIG0, config0);

    /* Feed once to start the countdown */
    REG_WRITE(TIMG0_WDTFEED, 1U);

    REG_WRITE(TIMG0_WDTWPROTECT, 0U);

    return BOOT_OK;
}

void esp_rwdt_deinit(void)
{
    wdt_disable(TIMG0_WDTCONFIG0, TIMG0_WDTWPROTECT);
}

void esp_rwdt_kick(void)
{
    REG_WRITE(TIMG0_WDTWPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(TIMG0_WDTFEED, 1U);
    REG_WRITE(TIMG0_WDTWPROTECT, 0U);
}

/**
 * @brief Suspend WDT for critical flash operations.
 *
 * Instead of disabling completely, we max out the prescaler to extend
 * the timeout window. This keeps the safety net active while allowing
 * blocking ROM erase operations (~45ms) to complete.
 */
void esp_rwdt_suspend(void)
{
    REG_WRITE(TIMG0_WDTWPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(TIMG0_WDTCONFIG1, 0xFFFFU << 16);
    REG_WRITE(TIMG0_WDTFEED, 1U);
    REG_WRITE(TIMG0_WDTWPROTECT, 0U);
}

void esp_rwdt_resume(void)
{
    REG_WRITE(TIMG0_WDTWPROTECT, VAL_WDT_UNLOCK);
    REG_WRITE(TIMG0_WDTCONFIG1, s_saved_prescaler << 16);
    REG_WRITE(TIMG0_WDTFEED, 1U);
    REG_WRITE(TIMG0_WDTWPROTECT, 0U);
}
