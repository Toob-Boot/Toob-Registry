# Contributing to the Toob Registry

This guide covers everything you need to publish a chip package, driver, toolchain, or architecture to the Toob Registry. After your contribution lands on `main`, any user can run `toob init --chip <your_chip>` and get a working bootloader project.

---

## How It All Fits Together

Toob-Boot is a secure bootloader that runs on bare metal before any OS. The Registry provides all hardware-specific pieces — the Core SDK provides the generic boot logic. At build time, the Toob CLI stitches them together:

```
toob-registry/                          Core SDK (toobloader/)
├── chips/esp32c6/                      ├── core/        ← Generic boot state machine
│   ├── hardware.json    ─── generates ──→ generated_boot_config.h
│   ├── chip_platform.c  ─── wires ──────→ boot_hal.h traits
│   ├── startup.c        ─── calls ──────→ boot_main()
│   └── chip_config.h    ─── derives from → generated macros
├── arch/riscv32/        ─── provides ───→ Timer, trap vector
├── soc/esp/include/     ─── provides ───→ Register macros, ROM typedefs
├── drivers/flash/...    ─── implements ─→ flash_hal_t
└── toolchains/riscv32-esp-elf/         └── cmake/       ← Build system
```

The key insight: **`hardware.json` is the single source of truth.** Every hardware constant flows through it — into generated C headers, linker scripts, and partition maps. Drivers and startup code consume only the generated macros, never hardcoded values.

---

## Chip Packages

A chip package lives in `chips/<chip_name>/` and contains 7 mandatory files. The Registry Builder rejects PRs where any are missing.

### `chip_manifest.json`

The identity card. Declares what architecture, toolchain, and source files belong to this chip.

```json
{
    "name": "esp32c6",
    "arch": "riscv32",
    "compiler_prefix": "riscv32-esp-elf-",
    "description": "Espressif ESP32-C6 (RISC-V, 4MB Flash, WiFi 6, BLE 5)",
    "version": "1.0.0",
    "min_core_sdk": "core/v0.0.1",
    "sources": {
        "startup":  "startup.c",
        "platform": "chip_platform.c",
        "config":   "chip_config.h",
        "linker":   "esp32c6_stage1.ld",
        "hardware": "hardware.json",
        "drivers": [
            "drivers/flash/esp_rom_spi/flash.c",
            "drivers/wdt/esp_rwdt_with_swd/wdt.c",
            "drivers/uart/esp_uart_v1/console.c",
            "drivers/rtc/esp_rtc_mem/confirm.c",
            "drivers/clock/esp_systimer/reset_reason.c"
        ],
        "extra": ["mock_efuse.c"]
    },
    "includes": ["soc/esp/include"]
}
```

**Validated by CI:** `name`, `arch`, `compiler_prefix` must be non-empty. Every path in `sources` must exist on disk. The `arch` must have a matching `arch/<name>/` directory. The `compiler_prefix` must match a registered toolchain.

**Manual review:** `min_core_sdk` is not validated — it's used as a fallback when the CLI resolves `core_sdk = "latest"`. Set it to the earliest Core SDK tag you've tested against.

**Versioning:** Set new packages to `1.0.0`. The SemVer Calculator bumps automatically on subsequent changes: value changes → PATCH, field additions → MINOR, field removals → MAJOR.

---

### `hardware.json`

The most important file. The Manifest Compiler reads it and generates `generated_boot_config.h` — every value becomes a `#define` in your build.

```json
{
    "chip_family": "esp32-c6",
    "flash": {
        "size": 4194304,
        "write_alignment": 4,
        "app_alignment": 65536,
        "xip_base": "0x42000000",
        "regions": [
            { "base": 0, "type": "reserved", "size": 262144 },
            { "base": 262144, "type": "writable", "sector_size": 4096, "count": 960 }
        ]
    },
    "memory": {
        "ram_base": "0x40800000",
        "ram_size": "0x8000"
    },
    "registers": {
        "uart0_base":          "0x60000000",
        "timg0_wdt_base":      "0x60008000",
        "pmu_base":            "0x600B0000",
        "rom_ptr_flash_erase": "0x40000144",
        "rom_ptr_flash_read":  "0x40000150"
    },
    "constants": {
        "cpu_freq_hz": 160000000,
        "val_wdt_unlock": "0x50D83AA1",
        "flash_erased_byte": 255,
        "has_swd": 1,
        "rst_poweron": 1
    }
}
```

#### Required vs. Optional Sections

| Section               | Required | Notes |
| --------------------- | -------- | ----- |
| `flash`               | ✅        | Size, alignment, regions. Generates `CHIP_FLASH_*` macros automatically. |
| `memory`              | ✅        | RAM base and size for linker script generation. |
| `registers`           | ✅        | At minimum: WDT base + UART base. |
| `constants`           | ✅        | At minimum: WDT unlock key. |
| `crypto_capabilities` | Optional | HW crypto flags and arena size. Core SDK defaults apply if omitted. |
| `multi_core`          | Optional | Coprocessor definitions and IPC mechanism. |

#### How Macros Are Generated

The Manifest Compiler applies strict prefix rules:

| JSON Section | Prefix       | `uart0_base`           | → | `CHIP_REG_UART0_BASE`    |
| ------------ | ------------ | ---------------------- | - | ------------------------ |
| `registers`  | `CHIP_REG_`  | `rom_ptr_flash_erase`  | → | `CHIP_REG_ROM_PTR_FLASH_ERASE` |
| `constants`  | `CHIP_`      | `cpu_freq_hz`          | → | `CHIP_CPU_FREQ_HZ`      |
| `flash.*`    | `CHIP_FLASH_`| *(auto from schema)*   | → | `CHIP_FLASH_TOTAL_SIZE`  |

> **Trap:** `flash.size`, `flash.write_alignment`, and `flash.regions[].sector_size` are emitted automatically as `CHIP_FLASH_TOTAL_SIZE`, `CHIP_FLASH_WRITE_ALIGNMENT`, and `CHIP_FLASH_MAX_SECTOR_SIZE`. Duplicating these in `constants` causes a redefinition error.

> **Trap:** Every constant you declare must be referenced by at least one `.c` file. The CLI's fail-fast validation aborts the build otherwise. This prevents configuration drift.

Keys prefixed with `rom_ptr_` are special — the SoC vendor header uses them to create typed function pointers to BootROM routines (e.g. `ROM_FLASH_ERASE`).

---

### `startup.c`

The first code that runs after BootROM handoff. Responsibilities:

1. Set up the stack pointer
2. Clear BSS
3. Sterilize **all** watchdog timers (prevents resets during init)
4. Install the trap/exception vector
5. Call `boot_platform_init()` → `boot_main()` → jump to OS

Must `#include "chip_config.h"` and `"generated_boot_config.h"`.

---

### `chip_platform.c`

The HAL wiring layer. Implements `boot_platform_init()` which returns a `boot_platform_t` containing function pointers to all hardware traits:

| Trait          | Required | Typical Driver            |
| -------------- | -------- | ------------------------- |
| `flash_hal`    | ✅        | `drivers/flash/...`       |
| `confirm_hal`  | ✅        | `drivers/rtc/...`         |
| `crypto_hal`   | ✅        | Core SDK (Monocypher)     |
| `clock_hal`    | ✅        | `drivers/clock/...`       |
| `wdt_hal`      | ✅        | `drivers/wdt/...`         |
| `console_hal`  | Optional | `drivers/uart/...`        |
| `soc_hal`      | Optional | Implemented inline        |

Optional traits can be `NULL`. The Core SDK handles missing console/soc gracefully.

---

### `chip_config.h`

A compatibility shim that derives register offsets from the generated base addresses:

```c
#include "generated_boot_config.h"

#define REG_TIMG0_WDT_CONFIG0   (CHIP_REG_TIMG0_WDT_BASE + 0x48U)
#define REG_TIMG0_WDT_FEED      (CHIP_REG_TIMG0_WDT_BASE + 0x60U)
#define REG_RESET_CAUSE         (CHIP_REG_PMU_BASE + CHIP_RST_CAUSE_OFFSET)
```

Never hardcode raw addresses here — always derive from `CHIP_REG_*` bases.

---

### Linker Script (`<chip>_stage1.ld`)

Defines MEMORY regions and SECTIONS. Must include the auto-generated partition symbols:

```ld
INCLUDE generated_memory.ld
ENTRY(_start)

MEMORY
{
    iram (rwx)  : ORIGIN = 0x40800000, LENGTH = 0x7C000
    lp_ram (rw) : ORIGIN = 0x50000000, LENGTH = 0x4000
}
```

The MEMORY regions must match the chip's Technical Reference Manual exactly.

---

### Optional Chip Files

**`template_device.toml`** — The starter project that `toob init --chip <name>` copies. Without it, `toob init` generates a minimal skeleton with only `[device] chip = "<name>"`. Strongly recommended — it sets sensible defaults for partition sizes, boot config, and baud rate.

**`mock_efuse.c`** — Test-time mock for eFuse reads (built with `-DTOOB_MOCK_EFUSES=1`). Listed in `sources.extra`.

---

### Chip Variations

Not every chip follows the ESP32 pattern. Here's how to handle common differences:

| Situation | Adaptation |
| --------- | ---------- |
| No BootROM flash functions | Flash driver implements raw SPI commands directly. Omit `rom_ptr_*` from `registers`. |
| No LP_RAM / RTC memory | Confirm HAL uses a reserved flash sector or backup registers instead. |
| NAND flash (vs. NOR) | Adjust `flash.regions` with appropriate sector/page sizes. Flash driver handles ECC internally. |
| No hardware crypto | Core SDK's Monocypher (software) handles everything. Set `crypto_capabilities.hw_*` to `false`. |
| Single-core, no coprocessor | Omit `multi_core` from `hardware.json`. Set `soc_hal` to `NULL`. |

---

## Drivers

Drivers are reusable hardware implementations. A single driver can serve multiple chips — `esp_rom_spi` works for ESP32-C3, C6, and S3.

### Structure

```
drivers/<category>/<driver_name>/
├── driver_manifest.json    ← Required
└── <implementation>.c      ← Required
```

### `driver_manifest.json`

```json
{
  "name": "esp_rom_spi",
  "author": "your-name",
  "version": "1.0.0",
  "description": "ESP32 SPI Flash Driver via ROM pointers"
}
```

**Validated by CI:** `name` and `version` must be non-empty. The category (parent directory) must be registered in `driver_categories.json`:

```
clock · rtc · storage · crypto · console · power · wdt · bus · network · display · sensor · flash · uart
```

New categories require adding them to `driver_categories.json` first.

### HAL Trait Contract

Each driver implements functions for one HAL trait from `boot_hal.h`:

| Category | Trait           | Functions |
| -------- | --------------- | --------- |
| `flash`  | `flash_hal_t`   | `init`, `deinit`, `read`, `write`, `erase_sector`, `get_sector_size`, `get_last_vendor_error` |
| `wdt`    | `wdt_hal_t`     | `init(timeout_ms)`, `deinit`, `kick`, `suspend_for_critical_section`, `resume` |
| `uart`   | `console_hal_t` | `init(baudrate)`, `deinit`, `putchar`, `getchar`, `flush` |
| `clock`  | `clock_hal_t`   | `init`, `deinit`, `get_tick_ms`, `delay_ms`, `get_reset_reason` |
| `rtc`    | `confirm_hal_t` | `init`, `deinit`, `check_ok(nonce)`, `clear` |
| `crypto` | `crypto_hal_t`  | `init`, `deinit`, `hash_*`, `verify_ed25519`, `random`, `read_pubkey`, ... |

All `init()` return `boot_status_t`. All `deinit()` must be idempotent and `void`. The ABI version field must be `TOOB_HAL_ABI_V2`.

### Configurable Drivers (Optional)

Drivers can expose typed configuration parameters via `config_schema`:

```json
{
  "name": "esp_rwdt_with_swd",
  "version": "1.0.0",
  "description": "ESP32 RWDT with SWD sterilization",
  "config_schema": {
      "swd_enabled": { "type": "bool", "default": true, "description": "Enable SWD sterilization" }
  }
}
```

Chips override defaults in their `chip_manifest.json`:

```json
"driver_configs": {
    "esp_rwdt_with_swd": { "swd_enabled": false }
}
```

**Validated by CI:** Config keys must exist in the driver's `config_schema`. Types are enforced (`int`, `bool`, `string`, `hex`).

### Wiring a Driver

1. Reference the `.c` in `chip_manifest.json` → `sources.drivers`
2. Wire the function pointers in `chip_platform.c`

Drivers must consume `CHIP_*` / `CHIP_REG_*` macros — never hardcode hardware values.

---

## Toolchains

Toolchains define how Toob auto-provisions cross-compilers. If your chip uses a compiler not yet in the registry (e.g. `arm-none-eabi-` for Cortex-M), you need to add one.

### Structure

```
toolchains/<toolchain_name>/
├── toolchain_manifest.json    ← Required
└── toolchain.cmake            ← Required
```

### `toolchain_manifest.json`

```json
{
    "version": "1.0.0",
    "upstream_version": "13.2.0_20230928",
    "urls": {
        "linux_amd64":   "https://github.com/.../riscv32-...-x86_64-linux.tar.xz",
        "darwin_amd64":  "https://github.com/.../riscv32-...-x86_64-apple-darwin.tar.xz",
        "darwin_arm64":  "https://github.com/.../riscv32-...-aarch64-apple-darwin.tar.xz",
        "windows_amd64": "https://github.com/.../riscv32-...-x86_64-w64-mingw32.zip"
    },
    "sha256": {
        "linux_amd64":   "782feefe...",
        "darwin_amd64":  "e3b0c442...",
        "darwin_arm64":  "e3b0c442...",
        "windows_amd64": "1300a545..."
    }
}
```

**Validated by CI:** SHA256 hashes must be exactly 64 hex characters. Hashes for `linux_amd64`, `darwin_amd64`, and `windows_amd64` are mandatory. All download URLs should point to pinned releases, not `latest` redirects.

### `toolchain.cmake`

A CMake toolchain file for bare-metal cross-compilation. Must:

1. Set `CMAKE_SYSTEM_NAME Generic`
2. Define `CMAKE_C_COMPILER` using `TOOLCHAIN_PREFIX` (and `TOOLCHAIN_BIN_DIR` for hermetic builds)
3. Bypass CMake's compiler test: `set(CMAKE_C_COMPILER_WORKS TRUE)`
4. Set per-chip ISA flags (`-march`, `-mabi`)
5. Set bare-metal linker flags (`--specs=nano.specs`, `-nostartfiles`, `-Wl,--gc-sections`)

> **Important:** The current architecture uses chip-specific `if(TOOB_CHIP STREQUAL "esp32c6")` blocks inside `toolchain.cmake` to select ISA flags. When adding a new chip that reuses an existing toolchain, you must add an `elseif` block with the correct `-march`/`-mabi` for your chip. See `riscv32-esp-elf/toolchain.cmake` for the pattern.

### How Auto-Provisioning Works

When `toob build --native` runs and no toolchain is found locally:

1. CLI reads `compiler_prefix` from `chip_manifest.json` (e.g. `riscv32-esp-elf-`)
2. Strips the trailing `-` → toolchain name `riscv32-esp-elf`
3. Looks up `registry.json` → `toolchains` → downloads + verifies SHA256
4. Extracts to `~/.toob/toolchains/<name>/<upstream_version>/`

---

## Architecture Packages

Architecture packages provide ISA-level abstractions shared across all chips of the same instruction set. `arch/riscv32/` serves ESP32-C3, C6, and any future RISC-V chip.

### Structure

```
arch/<arch_name>/
├── arch_manifest.json    ← Required (name, version, description)
├── arch_timer.c          ← Required
├── arch_trap.c           ← Required
└── include/
    └── arch_<name>.h     ← Required
```

### Required Implementations

**`arch_timer.c`** — Timing primitives delegated to by `clock_hal_t`:

```c
void     arch_<name>_timer_init(uint32_t timer_base, uint32_t cpu_freq_hz);
uint32_t arch_<name>_get_tick_ms(void);
void     arch_<name>_delay_ms(uint32_t ms);
```

The chip passes its timer base address and frequency at init time — the arch code never hardcodes these.

**`arch_trap.c`** — Exception vector that dispatches hardware faults to `toob_ecc_trap()`. For RISC-V this is the `mtvec` handler; for ARM it would be `HardFault_Handler`. The contract from `boot_hal.h`: ECC/bit-rot exceptions must be caught and forwarded, never left to hang.

**`include/arch_<name>.h`** — Public header with CSR/register access macros, interrupt control, and declarations for the timer and trap APIs.

### Portability Rule

Architecture code must **never** reference chip-specific constants. Timer bases are parameters, not `#define`s. Chip quirks belong in the chip's `startup.c`.

---

## Vendor SoC Headers

The `soc/<vendor>/include/` directory contains headers shared across all chips from the same silicon vendor. For example, `soc/esp/include/esp_common.h` is used by every Espressif chip.

These headers typically provide:

- Register access macros (`REG_READ`, `REG_WRITE`, `REG_SET_BIT`, `REG_CLR_BIT`)
- ROM function typedefs and typed pointer macros (e.g. `ROM_FLASH_ERASE`)
- Error conversion (`esp_rom_to_status()`)
- HAL API forward declarations for the vendor's drivers

There is no manifest file for SoC headers — they're referenced via `includes` in `chip_manifest.json`. The SoC header must `#include "generated_boot_config.h"` to access the `CHIP_REG_ROM_PTR_*` macros before evaluating `#ifdef` guards.

---

## Validation Pipeline

### What CI Catches Automatically

When you push to `main`, `build_registry.go` and `semver_calc.go` run:

| Check | Fatal? |
| ----- | ------ |
| All `sources.*` files exist on disk | ✅ FATAL |
| `name`, `arch`, `compiler_prefix` are non-empty | ✅ FATAL |
| `arch/<name>/` directory exists with valid `arch_manifest.json` | ✅ FATAL |
| `compiler_prefix` maps to a registered toolchain | ✅ FATAL |
| Driver category is registered in `driver_categories.json` | ✅ FATAL |
| `driver_manifest.json` / `arch_manifest.json` have `name` + `version` | ✅ FATAL |
| Toolchain SHA256 hashes are 64 hex chars for 3 required platforms | ✅ FATAL |
| `driver_configs` keys exist in driver's `config_schema` | ✅ FATAL |
| SemVer downgrade (version went backwards) | ✅ FATAL |

At **build time**, the CLI adds:

| Check | Fatal? |
| ----- | ------ |
| Every generated `CHIP_*` / `CHIP_REG_*` macro is consumed by ≥1 `.c` file | ✅ FATAL |

### What CI Does NOT Catch

These are your responsibility during manual review:

| Rule | Why It Can't Be Automated |
| ---- | ------------------------- |
| `startup.c` sterilizes all WDTs | Semantic — CI only checks file existence |
| `chip_platform.c` wires all mandatory HAL traits | A NULL pointer panics at runtime, not compile time |
| `chip_config.h` derives offsets instead of hardcoding | Coding convention |
| Linker script MEMORY regions match the TRM | Hardware-specific knowledge |
| `deinit()` is idempotent | Behavioral contract |
| No hardcoded values where a macro should be used | CI catches *unused* macros, not *missing* macro usage |
| `toolchain.cmake` supports hermetic paths | Falls back silently to PATH search |

### After Merge: Compatibility Matrix

After your PR lands on `main`, the CI server automatically queues a matrix build for your chip × CLI × compiler combination. Until that build passes, users see a non-blocking `⚠ not yet verified` warning. The chip's `verified` flag in `registry.json` is set to `true` only after CI validation succeeds.

### Common Errors

All validation errors are prefixed with `FATAL:` and are self-explanatory:

```
FATAL: Chip 'my_chip' is missing required fields.
FATAL: Chip 'my_chip' declares arch 'arm32', but directory 'arch/arm32' does not exist!
FATAL: Driver 'my_driver' uses illegal category 'gpio'. Must be registered in driver_categories.json!
FATAL: Toolchain 'arm-none-eabi' is missing a valid 64-character SHA256 hash for architecture 'linux_amd64'.
```

---

## Checklists

### Chip Package

**CI-Validated:**
- [ ] `chip_manifest.json` has `name`, `arch`, `compiler_prefix`, `sources`
- [ ] All `sources.*` file paths exist on disk
- [ ] All `sources.drivers[]` paths exist in the registry
- [ ] `arch/<name>/` exists with valid manifest
- [ ] `compiler_prefix` matches a registered toolchain
- [ ] Every `constants` entry is consumed by ≥1 `.c` file
- [ ] No duplication between `flash.*` fields and `constants` block

**Manual Review:**
- [ ] `hardware.json` values match the chip's TRM
- [ ] `startup.c` sterilizes all watchdogs and calls `boot_main()`
- [ ] `chip_platform.c` wires all 5 mandatory HAL traits
- [ ] `chip_config.h` derives offsets from `CHIP_REG_*` bases
- [ ] Linker script includes `generated_memory.ld` and has correct MEMORY regions
- [ ] `template_device.toml` included with sensible defaults
- [ ] Build passes: `toob build --native`

### Driver

- [ ] `driver_manifest.json` has `name`, `version`, `description` *(CI)*
- [ ] Placed in a valid category folder *(CI)*
- [ ] Follows HAL trait signature from `boot_hal.h` *(Manual)*
- [ ] Uses only generated macros — no hardcoded addresses *(Manual)*
- [ ] `deinit()` is idempotent *(Manual)*

### Toolchain

- [ ] `toolchain_manifest.json` has `version`, `upstream_version`, `urls`, `sha256` *(CI)*
- [ ] SHA256 hashes for `linux_amd64`, `darwin_amd64`, `windows_amd64` *(CI)*
- [ ] Download URLs are pinned releases *(Manual)*
- [ ] `toolchain.cmake` sets `CMAKE_SYSTEM_NAME Generic` *(Manual)*
- [ ] `toolchain.cmake` has `elseif` block for your chip's ISA flags *(Manual)*

### Architecture

- [ ] `arch_manifest.json` has `name`, `version`, `description` *(CI)*
- [ ] `arch_timer.c` implements `timer_init`, `get_tick_ms`, `delay_ms` *(Manual)*
- [ ] `arch_trap.c` dispatches to `toob_ecc_trap()` *(Manual)*
- [ ] No chip-specific `#define`s in architecture code *(Manual)*
