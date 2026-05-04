#ifndef CHIP_FAULT_INJECT_H
#define CHIP_FAULT_INJECT_H

#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>

/**
 * @brief Globale Konfiguration für aufgetretene Bit-Rot Injektionen und Brownouts
 */
typedef struct {
    uint32_t write_count_limit;
    uint32_t simulated_writes;
    uint32_t bitrot_addr;
    uint8_t  bitrot_value;
    const char *flash_sim_file;
    const char *rtc_sim_file;
    uint32_t simulated_reset_reason;
    bool wdt_disable_forbidden;
    bool crypto_bypass_ed25519;
    bool crypto_force_invalid;
    bool crypto_hardware_fault;
    uint32_t efuse_monotonic_limit;
    bool efuse_hardware_fault;
    const char *uart_rx_file;
    bool console_hardware_fault;
    uint32_t battery_level_mv;
} fault_inject_config_t;

extern fault_inject_config_t g_fault_config;

/**
 * @brief Liest Umgebungsvariablen wie TOOB_FAIL_AFTER_WRITES beim Start ein.
 */
void fault_inject_init(void);

/**
 * @brief Inkrementiert den Write-Counter und crasht gezielt MÄHREND des 
 *        gegebenen `fwrite` falls das Limit erreicht ist.
 * 
 * @param flash_file Der offene FILE* Pointer des Flash Mocks.
 * @param buffer Die zu schreibenden Daten.
 * @param chunk_size Die Größe des Buffers.
 */
void fault_inject_point_flash(FILE *flash_file, const void *buffer, size_t chunk_size);

/**
 * @brief Führt Bitrot auf dem eingelesenen Buffer aus, falls konfiguriert.
 */
void fault_inject_apply_bitrot(uint32_t addr, void *buf, size_t len);

#endif /* CHIP_FAULT_INJECT_H */
