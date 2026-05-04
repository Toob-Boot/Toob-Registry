/*
 * ==============================================================================
 * Toob-Boot Link-Time Mocking: ESP32-C6 eFuses
 * ==============================================================================
 * 
 * NASA P10 Anti-Brick Strategy:
 * Diese Datei wird NUR einkompiliert, wenn TOOB_MOCK_EFUSES definiert ist.
 * Sie nutzt die GNU Linker `--wrap` Funktion, um physische Register-Zugriffe
 * aus `chip_platform.c` auf RAM-basierte Mocks umzuleiten.
 * 
 * ==============================================================================
 */

#ifdef TOOB_MOCK_EFUSES

#include "boot_hal.h"
#include <string.h>

boot_status_t __wrap_esp32c6_read_pubkey(uint8_t *key, size_t key_len, uint8_t key_index);
boot_status_t __wrap_esp32c6_read_dslc(uint8_t *buffer, size_t *len);
boot_status_t __wrap_esp32c6_read_monotonic(uint32_t *ctr);
boot_status_t __wrap_esp32c6_advance_monotonic(void);

/* Dummy Ed25519 Root Key (RFC 8032 Test Vector 1) */
static const uint8_t MOCK_ED_PUBKEY[32] = {
    0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7,
    0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64, 0x07, 0x3a,
    0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25,
    0xaf, 0x02, 0x1a, 0x68, 0xf7, 0x07, 0x51, 0x1a
};

/* Fallback Ed25519 Root Key für Rotation Tests (key_index > 0, RFC 8032 Test Vector 2) */
static const uint8_t MOCK_ED_PUBKEY_FALLBACK[32] = {
    0x3d, 0x40, 0x17, 0xc3, 0xe8, 0x43, 0x89, 0x5a,
    0x92, 0xb7, 0x0a, 0xa7, 0x4d, 0x1b, 0x7e, 0xbc,
    0x9c, 0x98, 0x2c, 0xcf, 0x2e, 0xc4, 0x96, 0x8c,
    0xe0, 0xeb, 0x66, 0x84, 0x78, 0xd2, 0x2a, 0x86
};

/* Dummy DSLC (32 Bytes, P10 Alignment für boot_state.c) */
static const uint8_t MOCK_DSLC_BYTES[32] = "M-SANDBOX-DSLC-0000000000000001";

boot_status_t __wrap_esp32c6_read_pubkey(uint8_t *key, size_t key_len, uint8_t key_index)
{
    if (!key || key_len != 32) {
        return BOOT_ERR_INVALID_ARG;
    }
    
    if (key_index == 0) {
        memcpy(key, MOCK_ED_PUBKEY, 32);
    } else {
        memcpy(key, MOCK_ED_PUBKEY_FALLBACK, 32);
    }
    
    return BOOT_OK;
}

boot_status_t __wrap_esp32c6_read_dslc(uint8_t *buffer, size_t *len)
{
    if (!buffer || !len || *len < 32) {
        return BOOT_ERR_INVALID_ARG;
    }
    memcpy(buffer, MOCK_DSLC_BYTES, 32);
    *len = 32;
    return BOOT_OK;
}

boot_status_t __wrap_esp32c6_read_monotonic(uint32_t *ctr)
{
    if (!ctr) {
        return BOOT_ERR_INVALID_ARG;
    }
    *ctr = 10; /* Dummy Counter */
    return BOOT_OK;
}

boot_status_t __wrap_esp32c6_advance_monotonic(void)
{
    /* Mocks den erfolgreichen Schreibvorgang, 
       ohne physischen Flash-Verschleiß. */
    return BOOT_OK;
}

#endif /* TOOB_MOCK_EFUSES */
