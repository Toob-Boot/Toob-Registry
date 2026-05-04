/**
 * ==============================================================================
 * Toob-Boot M-SANDBOX: Chip Platform Wiring
 * ==============================================================================
 * 
 * REFERENCED SPECIFICATIONS & GAPS:
 * 1. docs/structure_plan.md
 * 2. docs/hals.md
 * 3. docs/concept_fusion.md
 */

#include "boot_hal.h"
#include "chip_config.h"

#include "mock_flash.h"
#include "mock_rtc_ram.h"
#include "mock_wdt.h"
#include "mock_clock.h"
#include "mock_console.h"
#include "mock_soc.h"
#include "mock_crypto_policy.h"
#include "mock_efuses.h"

#include "../../../crypto/monocypher/crypto_monocypher.h"
#include <stdlib.h>

/* -- Generischer Host Zufallsgenerator (Mock) -- */
static boot_status_t sandbox_random(uint8_t *buffer, size_t len) {
    for (size_t i = 0; i < len; i++) {
        buffer[i] = (uint8_t)(rand() % 256);
    }
    return BOOT_OK;
}

/* -- Architektur Zusammenbau für Sandbox Crypto HAL -- */
static crypto_hal_t sandbox_crypto_hal = {
    .abi_version              = 0x01000000,
    .init                     = crypto_monocypher_init,
    .deinit                   = crypto_monocypher_deinit,
    .hash_init                = crypto_monocypher_hash_init,
    .hash_update              = crypto_monocypher_hash_update,
    .hash_finish              = crypto_monocypher_hash_finish,
    .verify_ed25519           = crypto_monocypher_verify,
    .verify_pqc               = NULL,
    .random                   = sandbox_random,
    .get_last_vendor_error    = NULL,     /* Mock injected via linker wrap or omitted */
    .read_pubkey              = mock_efuse_read_pubkey,
    .read_dslc                = mock_efuse_read_dslc,
    .read_monotonic_counter   = mock_efuse_read_monotonic_counter,
    .advance_monotonic_counter= mock_efuse_advance_monotonic_counter,
    .get_hash_ctx_size        = crypto_monocypher_get_hash_ctx_size,
    .has_hw_acceleration      = false,
    .is_pqc_enforced          = NULL,
};

/* -- Globale Platform Handoff Struktur -- */
static boot_platform_t platform = {
    .flash   = &sandbox_flash_hal,
    .confirm = &sandbox_confirm_hal,
    .crypto  = &sandbox_crypto_hal,
    .clock   = &sandbox_clock_hal,
    .wdt     = &sandbox_wdt_hal,
    .console = &sandbox_console_hal,
    .soc     = &sandbox_soc_hal,
};

const boot_platform_t *boot_platform_init(void) {
    return &platform;
}
