/**
 * ==============================================================================
 * Toob-Boot M-SANDBOX: Static Chip Configuration
 * ==============================================================================
 *
 * REFERENCED SPECIFICATIONS & GAPS:
 *
 * 1. docs/toobfuzzer_integration.md
 *    - GAP-F19: "Sandbox-Defaults" - Da die Sandbox (POSIX) nicht iterativ vom
 *      Toobfuzzer gescannt werden kann, MÜSSEN hier alle Limitierungen aus dem
 *      blueprint.json / aggregated_scan.json als statische Worst-Case Hardware
 *      Metriken verankert werden, andernfalls bricht der C-Core beim Zugriff
 *      auf undefinierte Konstanten.
 *    - GAP-40, F04: BOOT_WDT_TIMEOUT_MS wird strikt als ganzzahliger Wert
 * gesetzt.
 *    - GAP-F07: CHIP_FLASH_ERASURE_MAPPING = 0xFF simuliert NOR-Flash Physik.
 *
 * 2. docs/concept_fusion.md
 *    - WDT Quantum: Der Timeout sichert Integrationstests vor Deadlocks.
 *
 * 3. docs/structure_plan.md
 *    - Strict C17 Include Guards.
 */

#ifndef CHIP_CONFIG_SANDBOX_H
#define CHIP_CONFIG_SANDBOX_H

#include <stdint.h>

/* --- Flash Typologie (Simuliert: 16 MB NOR-Flash) --- */
#define CHIP_FLASH_TOTAL_SIZE (16 * 1024 * 1024)
#define CHIP_FLASH_MAX_SECTOR_SIZE 4096 /* Toobfuzzer Segments O(n) Maximum */
#define CHIP_FLASH_PAGE_SIZE 4096       /* Für simple Block-Erase Sandbox Map */
#define CHIP_FLASH_WRITE_ALIGNMENT 4    /* Alias gemäß Fuzzer-Blueprint F18 */
#define CHIP_APP_ALIGNMENT_BYTES 65536  /* Sub-App Execute-In-Place Grenze */
#define CHIP_FLASH_ERASURE_MAPPING                                             \
  0xFF /* Klassischer Zustand nach Erase-Cycle */

/* (GAP-C01 Mitigation): TOOB_FLASH_DISABLE_BLANK_CHECK kann hier als
 * lokales Opt-In definiert werden, um den strikten O(1) Erase-Verify-Check in
 * mock_flash.c abzuschalten. Standardmäßig aktiv für sichere Tests. */
#define TOOB_FLASH_DISABLE_BLANK_CHECK 1

/* --- Hardware Watchdog Limits (Host-Mock) --- */
#define BOOT_WDT_TIMEOUT_MS 5000 /* Inkl. timing_safety_factor */

/* --- Battery Guard (Host-Mock) --- */
#define CHIP_MIN_BATTERY_MV 3300 /* Aus Sandbox-Metadaten / device.toml */

/* --- ROM-Pointer / Hardware-Register (In Sandbox nicht existent) --- */
/* (Der Mock greift nicht darauf zu, da er direkt POSIX/stdlib benutzt) */

#endif /* CHIP_CONFIG_SANDBOX_H */
