/**
 * @file chip_platform.c
 * @brief ESP32-C6 Platform Wiring — Connects arch/vendor layers to 7 HAL Traits
 *
 * This is the central integration point for the ESP32-C6 target.
 * It instantiates each HAL trait struct with the vendor-layer function pointers
 * and the chip_config.h hardware constants, then returns the assembled
 * boot_platform_t to the core state machine.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md
 *    - Init order: clock → flash → wdt → crypto → confirm → console → soc
 *    - "Wenn ein PFLICHT-HAL init() fehlschlaegt, muss der Bootloader atomar panicken."
 * 2. docs/structure_plan.md
 *    - chip_platform.c is the sole boot_platform_init() implementation per target.
 * 3. docs/concept_fusion.md Section 6
 *    - Hybrid architecture: bare-metal registers + ROM pointers.
 *    - crypto_hal_t uses Monocypher (software) — no HW SHA on C6 BootROM.
 */

#include "boot_hal.h"
#include "boot_panic.h"
#include "chip_config.h"

#include "arch_riscv.h"
#include "esp_common.h"
#include "../../../crypto/monocypher/crypto_monocypher.h"

#include <stddef.h>

/* ========================================================================
 * Flash HAL (vendor_spi_flash.c)
 * ======================================================================== */
static const flash_hal_t esp32c6_flash_hal = {
    .abi_version          = TOOB_HAL_ABI_V2,
    .init                 = esp_flash_init,
    .deinit               = esp_flash_deinit,
    .read                 = esp_flash_read,
    .write                = esp_flash_write,
    .erase_sector         = esp_flash_erase_sector,
    .get_sector_size      = esp_flash_get_sector_size,
    .set_otfdec_mode      = NULL,
    .get_last_vendor_error = esp_flash_get_vendor_error,
    .max_sector_size      = CHIP_FLASH_MAX_SECTOR_SIZE,
    .total_size           = CHIP_FLASH_TOTAL_SIZE,
    .max_erase_cycles     = 100000U,
    .write_align          = CHIP_FLASH_WRITE_ALIGNMENT,
    .erased_value         = CHIP_FLASH_ERASED_BYTE,
};

/* ========================================================================
 * Confirm HAL (vendor_rtc_mem.c → LP_RAM at 0x50000000)
 * ======================================================================== */
static const confirm_hal_t esp32c6_confirm_hal = {
    .abi_version = TOOB_HAL_ABI_V2,
    .init        = esp_confirm_init,
    .deinit      = esp_confirm_deinit,
    .check_ok    = esp_confirm_check_ok,
    .clear       = esp_confirm_clear,
};

/* ========================================================================
 * Watchdog HAL (vendor_rwdt.c → TIMG0 RWDT)
 * ======================================================================== */
static const wdt_hal_t esp32c6_wdt_hal = {
    .abi_version                   = TOOB_HAL_ABI_V2,
    .init                          = esp_rwdt_init,
    .deinit                        = esp_rwdt_deinit,
    .kick                          = esp_rwdt_kick,
    .suspend_for_critical_section  = esp_rwdt_suspend,
    .resume                        = esp_rwdt_resume,
};

/* ========================================================================
 * Clock HAL (arch_timer.c + vendor_reset_reason.c)
 * ======================================================================== */
static boot_status_t esp32c6_clock_init(void)
{
    arch_riscv_timer_init(CHIP_SYSTIMER_BASE, CHIP_CPU_FREQ_HZ);
    return BOOT_OK;
}

static void esp32c6_clock_deinit(void)
{
    /* SYSTIMER stays active for OS handoff */
}

static const clock_hal_t esp32c6_clock_hal = {
    .abi_version      = TOOB_HAL_ABI_V2,
    .init             = esp32c6_clock_init,
    .deinit           = esp32c6_clock_deinit,
    .get_tick_ms      = arch_riscv_get_tick_ms,
    .delay_ms         = arch_riscv_delay_ms,
    .get_reset_reason = esp_get_reset_reason,
};

/* ========================================================================
 * Console HAL (vendor_console.c → UART0)
 * ======================================================================== */
static boot_status_t esp32c6_console_init(uint32_t baudrate)
{
    return esp_uart_init(baudrate);
}

static const console_hal_t esp32c6_console_hal = {
    .abi_version = TOOB_HAL_ABI_V2,
    .init        = esp32c6_console_init,
    .deinit      = esp_uart_deinit,
    .putchar     = esp_uart_putchar,
    .getchar     = esp_uart_getchar,
    .flush       = esp_uart_flush,
};

/* ========================================================================
 * Crypto HAL (Monocypher Software — no HW SHA in ESP32-C6 BootROM)
 *
 * TODO: eFuse read_pubkey/read_dslc/monotonic_counter are stubbed.
 *       Real eFuse access requires EFUSE controller register mapping.
 * ======================================================================== */

/* ESP32-C6 Hardware RNG: RNG_DATA_REG at 0x60026000 (TRM Section 26.3).
 * Reading this 32-bit register returns entropy-mixed random data.
 * The ADC SAR noise source is always active on C6 during normal boot. */
#define REG_RNG_DATA  0x60026000U

static boot_status_t esp32c6_hw_random(uint8_t *buf, size_t len)
{
    if (!buf || len == 0) {
        return BOOT_ERR_INVALID_ARG;
    }

    size_t pos = 0;
    while (pos < len) {
        uint32_t word = REG_READ(REG_RNG_DATA);
        size_t remaining = len - pos;
        size_t chunk = (remaining >= 4U) ? 4U : remaining;
        for (size_t i = 0; i < chunk; i++) {
            buf[pos + i] = (uint8_t)(word >> (i * 8U));
        }
        pos += chunk;
    }
    return BOOT_OK;
}

/* EFUSE Controller Base Address for ESP32-C6 */
#define ESP32C6_EFUSE_BASE 0x600B0800U
/* Block 4 is commonly used for secure boot keys (KEY0) */
#define EFUSE_BLK_KEY0_DATA0_REG (ESP32C6_EFUSE_BASE + 0x0A4U)
/* SYS_DATA_PART2 is often used for custom counters (Block 2) */
#define EFUSE_SYS_DATA_PART2_REG (ESP32C6_EFUSE_BASE + 0x04CU)

static boot_status_t esp32c6_read_pubkey(uint8_t *key, size_t key_len, uint8_t key_index)
{
    (void)key_index; /* Selects between KEY0-KEY5. We statically map to KEY0. */
    if (!key || key_len != 32) {
        return BOOT_ERR_INVALID_ARG;
    }

    /* Production P10 Physical Access: Direct memory mapped hardware registers */
    for (size_t i = 0; i < 8; i++) {
        uint32_t word = REG_READ(EFUSE_BLK_KEY0_DATA0_REG + (i * 4U));
        key[(i * 4) + 0] = (uint8_t)(word & 0xFF);
        key[(i * 4) + 1] = (uint8_t)((word >> 8) & 0xFF);
        key[(i * 4) + 2] = (uint8_t)((word >> 16) & 0xFF);
        key[(i * 4) + 3] = (uint8_t)((word >> 24) & 0xFF);
    }
    return BOOT_OK;
}

static boot_status_t esp32c6_read_dslc(uint8_t *buffer, size_t *len)
{
    if (!buffer || !len || *len < 1) {
        return BOOT_ERR_INVALID_ARG;
    }
    /* Read lowest byte of SYS_DATA_PART2 as DSLC */
    uint32_t word = REG_READ(EFUSE_SYS_DATA_PART2_REG);
    buffer[0] = (uint8_t)(word & 0xFF);
    *len = 1;
    return BOOT_OK;
}

static boot_status_t esp32c6_read_monotonic(uint32_t *ctr)
{
    if (!ctr) {
        return BOOT_ERR_INVALID_ARG;
    }
    /* Read from next word in SYS_DATA_PART2 as Monotonic Counter */
    *ctr = REG_READ(EFUSE_SYS_DATA_PART2_REG + 4U);
    return BOOT_OK;
}

static boot_status_t esp32c6_advance_monotonic(void)
{
    /* Physical write to eFuse requires timing/programming control (ets_efuse_program).
       For production bootloaders, advancing the monotonic counter is handled via ROM calls 
       or strictly regulated burn cycles to prevent brcking. */
    return BOOT_ERR_NOT_SUPPORTED;
}

static const crypto_hal_t esp32c6_crypto_hal = {
    .abi_version              = TOOB_HAL_ABI_V2,
    .init                     = crypto_monocypher_init,
    .deinit                   = crypto_monocypher_deinit,
    .hash_init                = crypto_monocypher_hash_init,
    .hash_update              = crypto_monocypher_hash_update,
    .hash_finish              = crypto_monocypher_hash_finish,
    .verify_ed25519           = crypto_monocypher_verify,
    .verify_pqc               = NULL,
    .random                   = esp32c6_hw_random,
    .get_last_vendor_error    = NULL,
    .read_pubkey              = esp32c6_read_pubkey,
    .read_dslc                = esp32c6_read_dslc,
    .read_monotonic_counter   = esp32c6_read_monotonic,
    .advance_monotonic_counter = esp32c6_advance_monotonic,
    .get_hash_ctx_size        = crypto_monocypher_get_hash_ctx_size,
    .has_hw_acceleration      = false,
    .is_pqc_enforced          = NULL,
};

/* ========================================================================
 * Platform Assembly
 * ======================================================================== */
static const boot_platform_t platform = {
    .flash   = &esp32c6_flash_hal,
    .confirm = &esp32c6_confirm_hal,
    .crypto  = &esp32c6_crypto_hal,
    .clock   = &esp32c6_clock_hal,
    .wdt     = &esp32c6_wdt_hal,
    .console = &esp32c6_console_hal,
    .soc     = NULL,
};

/**
 * @brief ESP32-C6 boot_platform_init implementation.
 *
 * Follows the mandatory init order from hals.md:
 * ① clock → ② flash → ③ wdt → ④ crypto → ⑤ confirm → ⑥ console
 *
 * Any mandatory HAL init failure triggers boot_panic().
 */
const boot_platform_t *boot_platform_init(void)
{
    arch_riscv_disable_interrupts();

    /* ① Clock: everything else needs a timebase */
    if (platform.clock->init() != BOOT_OK) {
        boot_panic(&platform, BOOT_ERR_STATE);
    }

    /* ② Flash: WAL + partition reads */
    if (platform.flash->init() != BOOT_OK) {
        boot_panic(&platform, BOOT_ERR_FLASH);
    }

    /* ③ WDT: as early as possible for crash recovery */
    if (platform.wdt->init(BOOT_WDT_TIMEOUT_MS) != BOOT_OK) {
        boot_panic(&platform, BOOT_ERR_STATE);
    }

    /* ④ Crypto: Monocypher software init */
    if (platform.crypto->init() != BOOT_OK) {
        boot_panic(&platform, BOOT_ERR_CRYPTO);
    }

    /* ⑤ Confirm: LP_RAM — always succeeds */
    if (platform.confirm->init() != BOOT_OK) {
        boot_panic(&platform, BOOT_ERR_STATE);
    }

    /* ⑥ Console: optional — failure is non-fatal */
    if (platform.console != NULL) {
        (void)platform.console->init(115200U);
    }

    return &platform;
}
