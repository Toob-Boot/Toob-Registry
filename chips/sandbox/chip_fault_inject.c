#include "chip_fault_inject.h"
#include <stdlib.h>
#include <string.h>

fault_inject_config_t g_fault_config = {
    .write_count_limit = 0,
    .simulated_writes = 0,
    .bitrot_addr = 0xFFFFFFFF,
    .bitrot_value = 0x00,
    .flash_sim_file = "flash_sim.bin",
    .rtc_sim_file = "rtc_sim.bin",
    .simulated_reset_reason = 1, /* RESET_REASON_POWER_ON */
    .wdt_disable_forbidden = false,
    .crypto_bypass_ed25519 = false,
    .crypto_force_invalid = false,
    .crypto_hardware_fault = false,
    .efuse_monotonic_limit = 256,
    .efuse_hardware_fault = false,
    .uart_rx_file = "uart_sim.bin",
    .console_hardware_fault = false,
    .battery_level_mv = 3700
};

void fault_inject_init(void) {
    const char *env_file = getenv("TOOB_FLASH_SIM_FILE");
    if (env_file != NULL) {
        g_fault_config.flash_sim_file = env_file;
    }

    const char *env_rtc = getenv("TOOB_RTC_SIM_FILE");
    if (env_rtc != NULL) {
        g_fault_config.rtc_sim_file = env_rtc;
    }

    const char *env_fail = getenv("TOOB_FAIL_AFTER_WRITES");
    if (env_fail != NULL) {
        g_fault_config.write_count_limit = (uint32_t)strtoul(env_fail, NULL, 10);
    }

    const char *env_bitrot_addr = getenv("TOOB_BITROT_ADDR");
    if (env_bitrot_addr != NULL) {
        g_fault_config.bitrot_addr = (uint32_t)strtoul(env_bitrot_addr, NULL, 16);
    }

    const char *env_bitrot_val = getenv("TOOB_BITROT_VALUE");
    if (env_bitrot_val != NULL) {
        g_fault_config.bitrot_value = (uint8_t)strtoul(env_bitrot_val, NULL, 16);
    }

    const char *env_reset = getenv("TOOB_RESET_REASON");
    if (env_reset != NULL) {
        g_fault_config.simulated_reset_reason = (uint32_t)strtoul(env_reset, NULL, 10);
    }

    const char *env_wdt = getenv("TOOB_WDT_DISABLE_FORBIDDEN");
    if (env_wdt != NULL) {
        g_fault_config.wdt_disable_forbidden = (bool)strtol(env_wdt, NULL, 10);
    }

    const char *env_cbypass = getenv("TOOB_CRYPTO_BYPASS");
    if (env_cbypass != NULL) {
        g_fault_config.crypto_bypass_ed25519 = (bool)strtol(env_cbypass, NULL, 10);
    }

    const char *env_cforce = getenv("TOOB_CRYPTO_REJECT");
    if (env_cforce != NULL) {
        g_fault_config.crypto_force_invalid = (bool)strtol(env_cforce, NULL, 10);
    }

    const char *env_chw = getenv("TOOB_CRYPTO_HW_FAULT");
    if (env_chw != NULL) {
        g_fault_config.crypto_hardware_fault = (bool)strtol(env_chw, NULL, 10);
    }

    const char *env_elimit = getenv("TOOB_EFUSE_LIMIT");
    if (env_elimit != NULL) {
        g_fault_config.efuse_monotonic_limit = (uint32_t)strtoul(env_elimit, NULL, 10);
    }

    const char *env_efault = getenv("TOOB_EFUSE_FAULT");
    if (env_efault != NULL) {
        g_fault_config.efuse_hardware_fault = (bool)strtol(env_efault, NULL, 10);
    }

    const char *env_crx = getenv("TOOB_UART_RX_FILE");
    if (env_crx != NULL) {
        g_fault_config.uart_rx_file = env_crx;
    }

    const char *env_cfault = getenv("TOOB_CONSOLE_FAULT");
    if (env_cfault != NULL) {
        g_fault_config.console_hardware_fault = (bool)strtol(env_cfault, NULL, 10);
    }

    const char *env_bat = getenv("TOOB_BATTERY_MV");
    if (env_bat != NULL) {
        g_fault_config.battery_level_mv = (uint32_t)strtoul(env_bat, NULL, 10);
    }
}

void fault_inject_point_flash(FILE *flash_file, const void *buffer, size_t chunk_size) {
    if (g_fault_config.write_count_limit > 0) {
        if (g_fault_config.simulated_writes >= g_fault_config.write_count_limit) {
            /* Torn Write Simulation: Schreibe exakt die HÄLFTE des Buffers, 
               dann harter Crash um das WAL auf die Probe zu stellen. */
            size_t partial_size = chunk_size / 2;
            if (partial_size > 0 && buffer != NULL && flash_file != NULL) {
                (void)fwrite(buffer, 1, partial_size, flash_file);
                fflush(flash_file);
            }
            printf("[M-SANDBOX] BROWNOUT SIMULATED! Power loss after %u writes.\n", g_fault_config.simulated_writes);
            fflush(stdout);
            exit(1);
        }
    }
    g_fault_config.simulated_writes++;
}

void fault_inject_apply_bitrot(uint32_t addr, void *buf, size_t len) {
    if (g_fault_config.bitrot_addr >= addr && g_fault_config.bitrot_addr < (addr + len)) {
        uint8_t *byte_buf = (uint8_t *)buf;
        byte_buf[g_fault_config.bitrot_addr - addr] = g_fault_config.bitrot_value;
    }
}
