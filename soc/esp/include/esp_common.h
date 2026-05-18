/**
 * @file esp_common.h
 * @brief Shared Definitions for all Espressif Chips in Toob-Boot
 *
 * Provides register access macros, ROM function typedefs, and the
 * vendor error-to-boot_status_t conversion shared across ESP32, S3, C3, C6.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/concept_fusion.md Section 6
 *    - Hybrid architecture: bare-metal registers + ROM pointers.
 *    - "(GAP-F13) return_convention: ROM 0 = success"
 * 2. docs/hals.md
 *    - get_last_vendor_error(): "Cache and Clear" pattern.
 * 3. docs/toobfuzzer_integration.md
 *    - ROM pointer typedefs derived from blueprint.json flash_controller.
 */

#ifndef ESP_COMMON_H
#define ESP_COMMON_H

#include "boot_types.h"
#include <stdint.h>
#include <stddef.h>

/* --- Register Access Macros --- */

#define REG_WRITE(addr, val)  (*(volatile uint32_t *)(uintptr_t)(addr) = (val))
#define REG_READ(addr)        (*(volatile uint32_t *)(uintptr_t)(addr))
#define REG_SET_BIT(addr, b)  REG_WRITE((addr), REG_READ(addr) | (b))
#define REG_CLR_BIT(addr, b)  REG_WRITE((addr), REG_READ(addr) & ~(b))

/* --- ROM Function Typedefs (from blueprint.json flash_controller) --- */

typedef int (*esp_rom_spiflash_erase_sector_t)(uint32_t sector_number);
typedef int (*esp_rom_spiflash_write_t)(uint32_t dest_addr, const void *src, uint32_t len);
typedef int (*esp_rom_spiflash_unlock_t)(void);
typedef int (*esp_rom_spiflash_read_t)(uint32_t src_addr, void *dest, uint32_t len);

/* ESP BootROM return convention: 0 = success, non-zero = error */
#define ESP_ROM_OK 0

/**
 * @brief Convert an ESP ROM return code to boot_status_t.
 *
 * The ROM functions return 0 on success. Any non-zero value is a
 * vendor-specific error that gets cached for get_last_vendor_error().
 */
static inline boot_status_t esp_rom_to_status(int rom_rc)
{
    if (rom_rc == ESP_ROM_OK) {
        return BOOT_OK;
    }
    return BOOT_ERR_FLASH;
}

/* --- Vendor Flash HAL API (implemented in vendor_spi_flash.c) --- */

boot_status_t esp_flash_init(void);
void          esp_flash_deinit(void);
boot_status_t esp_flash_read(uint32_t addr, void *buf, size_t len);
boot_status_t esp_flash_write(uint32_t addr, const void *buf, size_t len);
boot_status_t esp_flash_erase_sector(uint32_t addr);
boot_status_t esp_flash_get_sector_size(uint32_t addr, size_t *size_out);
uint32_t      esp_flash_get_vendor_error(void);

/* --- Vendor Confirm HAL API (implemented in vendor_rtc_mem.c) --- */

boot_status_t esp_confirm_init(void);
void          esp_confirm_deinit(void);
bool          esp_confirm_check_ok(uint64_t expected_nonce);
boot_status_t esp_confirm_clear(void);

/* --- Vendor WDT HAL API (implemented in vendor_rwdt.c) --- */

boot_status_t esp_rwdt_init(uint32_t timeout_ms);
void          esp_rwdt_deinit(void);
void          esp_rwdt_kick(void);
void          esp_rwdt_suspend(void);
void          esp_rwdt_resume(void);

/* --- Vendor Reset Reason API (implemented in vendor_reset_reason.c) --- */

reset_reason_t esp_get_reset_reason(void);

/* --- Vendor Console HAL API (implemented in vendor_console.c) --- */

boot_status_t esp_uart_init(uint32_t baudrate);
void          esp_uart_deinit(void);
void          esp_uart_putchar(char c);
boot_status_t esp_uart_getchar(uint8_t *out, uint32_t timeout_ms);
void          esp_uart_flush(void);

#endif /* ESP_COMMON_H */
