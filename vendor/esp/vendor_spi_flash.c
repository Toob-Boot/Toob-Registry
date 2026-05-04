/**
 * @file vendor_spi_flash.c
 * @brief ESP SPI Flash HAL via BootROM Function Pointers
 *
 * Implements flash_hal_t operations by calling the ROM-resident SPI flash
 * functions at their verified addresses. No ESP-IDF dependency.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 1 (flash_hal_t)
 *    - Read: no alignment constraint, bounds-checked.
 *    - Write: MUST check alignment, MUST blank-check before write.
 *    - Erase: MUST verify sector alignment. Blocking for ~45ms.
 *    - get_last_vendor_error: cached, clear-on-read.
 * 2. docs/concept_fusion.md Section 6.2
 *    - ROM_PTR_FLASH_ERASE, ROM_PTR_FLASH_WRITE from chip_config.h.
 *    - "Wir schreiben niemals eigene Bare-Metal SPI-Flash-Treiber."
 * 3. docs/toobfuzzer_integration.md
 *    - Return convention: ROM 0 = success. Non-zero cached as vendor error.
 * 4. blueprints/quirks/esp32-c6.json
 *    - Verified ROM addresses: erase=0x40000144, write=0x4000014c, unlock=0x40000154.
 * 5. esp32c6.rom.ld (SVD/Swift reference)
 *    - Cross-verified: esp_rom_spiflash_erase_sector=0x40000144,
 *      esp_rom_spiflash_write=0x4000014c, esp_rom_spiflash_unlock=0x40000154.
 */

#include "esp_common.h"
#include "chip_config.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

static uint32_t s_last_vendor_error;

boot_status_t esp_flash_init(void)
{
    s_last_vendor_error = 0;

    int rc = ROM_PTR_FLASH_UNLOCK();
    if (rc != ESP_ROM_OK) {
        s_last_vendor_error = (uint32_t)rc;
        return BOOT_ERR_FLASH;
    }

    return BOOT_OK;
}

void esp_flash_deinit(void)
{
    /* No-Op. SPI flash peripheral remains active for OS handoff. */
}

/**
 * @brief Read from flash via memory-mapped XIP region.
 *
 * On ESP32-C6, flash is memory-mapped starting at CHIP_FLASH_XIP_BASE
 * (0x42000000). Direct memcpy is valid for reads.
 */
boot_status_t esp_flash_read(uint32_t addr, void *buf, size_t len)
{
    if (buf == NULL) {
        return BOOT_ERR_INVALID_ARG;
    }
    if (addr + len > CHIP_FLASH_TOTAL_SIZE) {
        return BOOT_ERR_FLASH_BOUNDS;
    }

    const void *src = (const void *)(uintptr_t)(CHIP_FLASH_XIP_BASE + addr);
    memcpy(buf, src, len);

    return BOOT_OK;
}

/**
 * @brief Write to flash via ROM function.
 *
 * Enforces write alignment and optional blank-check per hals.md spec.
 * ROM function writes max 256 bytes per SPI transaction — we chunk internally.
 */
boot_status_t esp_flash_write(uint32_t addr, const void *buf, size_t len)
{
    if (buf == NULL || len == 0U) {
        return BOOT_ERR_INVALID_ARG;
    }
    if ((addr % CHIP_FLASH_WRITE_ALIGNMENT) != 0U) {
        return BOOT_ERR_FLASH_ALIGN;
    }
    if ((len % CHIP_FLASH_WRITE_ALIGNMENT) != 0U) {
        return BOOT_ERR_FLASH_ALIGN;
    }
    if (addr + len > CHIP_FLASH_TOTAL_SIZE) {
        return BOOT_ERR_FLASH_BOUNDS;
    }

#if !defined(TOOB_FLASH_DISABLE_BLANK_CHECK) || (TOOB_FLASH_DISABLE_BLANK_CHECK == 0)
    /*
     * Blank-check: Verify target region is erased (all bytes == 0xFF).
     * 32-bit aligned word checks for speed per hals.md spec.
     */
    const volatile uint32_t *check_ptr =
        (const volatile uint32_t *)(uintptr_t)(CHIP_FLASH_XIP_BASE + addr);
    size_t word_count = len / sizeof(uint32_t);
    for (size_t i = 0; i < word_count; i++) {
        if (check_ptr[i] != 0xFFFFFFFFU) {
            return BOOT_ERR_FLASH_NOT_ERASED;
        }
    }
#endif

    /* ROM SPI write: chunked at 256 bytes (SPI page program limit) */
    const uint8_t *src = (const uint8_t *)buf;
    uint32_t remaining = (uint32_t)len;
    uint32_t offset = addr;

    while (remaining > 0U) {
        uint32_t chunk = (remaining > 256U) ? 256U : remaining;

        int rc = ROM_PTR_FLASH_WRITE(offset, src, chunk);
        if (rc != ESP_ROM_OK) {
            s_last_vendor_error = (uint32_t)rc;
            return BOOT_ERR_FLASH;
        }

        src       += chunk;
        offset    += chunk;
        remaining -= chunk;
    }

    return BOOT_OK;
}

/**
 * @brief Erase a single 4KB sector via ROM function.
 *
 * ROM expects the sector NUMBER (addr / 4096), not the byte address.
 */
boot_status_t esp_flash_erase_sector(uint32_t addr)
{
    if ((addr % CHIP_FLASH_MAX_SECTOR_SIZE) != 0U) {
        return BOOT_ERR_FLASH_ALIGN;
    }
    if (addr >= CHIP_FLASH_TOTAL_SIZE) {
        return BOOT_ERR_FLASH_BOUNDS;
    }

    uint32_t sector_num = addr / CHIP_FLASH_MAX_SECTOR_SIZE;
    int rc = ROM_PTR_FLASH_ERASE(sector_num);

    if (rc != ESP_ROM_OK) {
        s_last_vendor_error = (uint32_t)rc;
        return BOOT_ERR_FLASH;
    }

    return BOOT_OK;
}

/**
 * @brief Return uniform 4KB sector size (verified by toobfuzzer).
 */
boot_status_t esp_flash_get_sector_size(uint32_t addr, size_t *size_out)
{
    if (size_out == NULL) {
        return BOOT_ERR_INVALID_ARG;
    }
    if (addr >= CHIP_FLASH_TOTAL_SIZE) {
        return BOOT_ERR_FLASH_BOUNDS;
    }

    *size_out = CHIP_FLASH_MAX_SECTOR_SIZE;
    return BOOT_OK;
}

/**
 * @brief Return and clear the last vendor error code (Clear-on-Read).
 */
uint32_t esp_flash_get_vendor_error(void)
{
    uint32_t err = s_last_vendor_error;
    s_last_vendor_error = 0;
    return err;
}
