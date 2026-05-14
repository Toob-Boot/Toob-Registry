/**
 * Toob-Loader "Zero-Bloat" Linker Hooks
 */
#include "libtoob.h"
#include <stdio.h>

toob_status_t toob_os_flash_read(uint32_t addr, uint8_t* buf, uint32_t len) { return TOOB_ERR_NOT_SUPPORTED; }
toob_status_t toob_os_flash_write(uint32_t addr, const uint8_t* buf, uint32_t len) { return TOOB_ERR_NOT_SUPPORTED; }
toob_status_t toob_os_flash_erase(uint32_t addr, uint32_t len) { return TOOB_ERR_NOT_SUPPORTED; }
toob_status_t toob_os_sha256_init(toob_os_sha256_ctx_t* ctx) { return TOOB_ERR_NOT_SUPPORTED; }
toob_status_t toob_os_sha256_update(toob_os_sha256_ctx_t* ctx, const uint8_t* data, uint32_t len) { return TOOB_ERR_NOT_SUPPORTED; }
toob_status_t toob_os_sha256_finalize(toob_os_sha256_ctx_t* ctx, uint8_t out_hash[32]) { return TOOB_ERR_NOT_SUPPORTED; }
