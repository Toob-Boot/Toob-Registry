# Contributing a Chip Package to the Toob Registry

This guide explains how to add support for a new microcontroller to Toob-Boot by publishing a **Chip Package** to the `Toob-Registry`. After following these steps, any user can run `toob init --chip <your_chip>` and get a fully functional bootloader project.

---

## Architecture Overview

A Chip Package is not a single file — it is a set of interconnected artifacts spread across multiple registry directories. The build pipeline stitches them together at compile time:

```
toob-registry/
├── chips/<chip_name>/          ← Your chip package (the main deliverable)
├── arch/<arch_name>/           ← Reusable architecture layer (may already exist)
├── soc/<vendor>/include/       ← Shared vendor header (may already exist)
├── drivers/<category>/<name>/  ← Reusable drivers (may already exist)
└── toolchains/<toolchain>/     ← Cross-compiler definition (may already exist)
```

Before you start, check whether your chip's architecture (`riscv32`, `arm-none-eabi`, `xtensa`) and vendor SoC headers already exist. You may only need to add the `chips/<chip_name>/` directory.

---

## 1. Mandatory Files

Every chip package **must** contain these 7 files. The Registry Builder (`build_registry.go`) will reject your PR if any are missing.

### `chip_manifest.json`

The identity card of your chip. Declares the architecture, toolchain, source files, and driver wiring.

```json
{
    "arch": "riscv32",
    "compiler_prefix": "riscv32-esp-elf-",
    "description": "Espressif ESP32-C6 (RISC-V, 4MB Flash, WiFi 6, BLE 5)",
    "name": "esp32c6",
    "version": "1.0.0",
    "min_core_sdk": "core/v0.0.1",
    "min_compiler": "latest",
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
        "extra": [
            "mock_efuse.c"
        ]
    },
    "includes": [
        "soc/esp/include"
    ]
}
```

| Field              | Required | Description                                                              |
| ------------------ | -------- | ------------------------------------------------------------------------ |
| `name`             | ✅        | Directory name, used as the `--chip` CLI argument.                       |
| `arch`             | ✅        | Must match an `arch/<name>/` directory in the registry.                  |
| `compiler_prefix`  | ✅        | GCC triplet prefix (e.g. `riscv32-esp-elf-`). Must match a toolchain.   |
| `description`      | ✅        | One-line human-readable summary.                                         |
| `version`          | ✅        | SemVer. Bumped automatically by the SemVer Calculator on content change. |
| `min_core_sdk`     | ✅        | Minimum compatible Core SDK tag.                                         |
| `sources.startup`  | ✅        | Early init (BSS clear, stack, WDT sterilize, call `boot_main()`).       |
| `sources.platform` | ✅        | HAL trait wiring (`boot_platform_init()`).                               |
| `sources.config`   | ✅        | Compatibility shim header (register offset aliases).                     |
| `sources.linker`   | ✅        | Linker script defining MEMORY regions and SECTIONS.                      |
| `sources.hardware` | ✅        | Hardware JSON (see below).                                               |
| `sources.drivers`  | ✅        | List of driver `.c` paths (relative to registry root).                   |
| `includes`         | Optional | Additional include search paths (relative to registry root).             |
| `sources.extra`    | Optional | Additional chip-local source files (e.g. mock/test utilities).           |

---

### `hardware.json`

The **single source of truth** for all hardware parameters. The Manifest Compiler reads this file and generates `generated_boot_config.h` with C macros. Every value you put here becomes a `#define` in the build.

```json
{
    "chip_family": "esp32-c6",
    "flash": {
        "size": 4194304,
        "write_alignment": 4,
        "app_alignment": 65536,
        "xip_base": "0x42000000",
        "regions": [
            {
                "base": 0,
                "type": "reserved",
                "size": 262144,
                "description": "BootROM reserved"
            },
            {
                "base": 262144,
                "type": "writable",
                "sector_size": 4096,
                "count": 960
            }
        ]
    },
    "memory": {
        "ram_base": "0x40800000",
        "ram_size": "0x8000"
    },
    "registers": { ... },
    "constants": { ... }
}
```

#### Naming Rules (Critical!)

The Manifest Compiler applies strict prefix rules when generating macros:

| JSON Section   | Generated Prefix | Example JSON Key      | Generated Macro                |
| -------------- | ---------------- | --------------------- | ------------------------------ |
| `registers`    | `CHIP_REG_`      | `uart0_base`          | `CHIP_REG_UART0_BASE`         |
| `constants`    | `CHIP_`          | `cpu_freq_hz`         | `CHIP_CPU_FREQ_HZ`            |
| `flash.*`      | `CHIP_FLASH_`    | *(auto from schema)*  | `CHIP_FLASH_TOTAL_SIZE`, etc.  |

> **Important:** The `flash.size`, `flash.write_alignment`, and `flash.regions[].sector_size` fields are automatically emitted by the generator as `CHIP_FLASH_TOTAL_SIZE`, `CHIP_FLASH_WRITE_ALIGNMENT`, and `CHIP_FLASH_MAX_SECTOR_SIZE`. Do **not** duplicate these in the `constants` block — the compiler will abort with a redefinition error.

#### `registers` Block

Physical memory-mapped register base addresses. Your drivers read these via `REG_READ()` / `REG_WRITE()` macros.

```json
"registers": {
    "uart0_base":          "0x60000000",
    "timg0_wdt_base":      "0x60008000",
    "pmu_base":            "0x600B0000",
    "rom_ptr_flash_erase": "0x40000144",
    "rom_ptr_flash_read":  "0x40000150"
}
```

Keys prefixed with `rom_ptr_` are special — they are used by the SoC vendor header (`esp_common.h`) to create typed function pointers to BootROM routines.

#### `constants` Block

Chip-specific numeric constants consumed by drivers. Every entry here must be referenced by at least one `.c` file in the build, or the Manifest Compiler's **fail-fast validation** will reject the build.

```json
"constants": {
    "cpu_freq_hz": 160000000,
    "uart_sclk_freq": 40000000,
    "uart_tx_fifo_size": 128,
    "flash_erased_byte": 255,
    "val_wdt_unlock": "0x50D83AA1",
    "has_swd": 1,
    "rst_poweron": 1,
    "rst_sw_sys": 3
}
```

#### Optional Sections

| Section                | Purpose                                              |
| ---------------------- | ---------------------------------------------------- |
| `crypto_capabilities`  | Declares hardware crypto accelerators (AES, SHA, PKA) and arena size. |
| `multi_core`           | Coprocessor definitions and IPC mechanism.           |

---

### `startup.c`

The absolute first code that runs after BootROM handoff. Must:

1. Set up the stack pointer (architecture-specific)
2. Clear the BSS segment
3. Sterilize **all** watchdog timers
4. Install the trap/exception vector
5. Call `boot_platform_init()` → `boot_main()` → jump to OS

This file must `#include "chip_config.h"` and `"generated_boot_config.h"` to access register addresses.

---

### `chip_platform.c`

The HAL wiring layer. Implements `boot_platform_init()` which returns a `boot_platform_t` struct containing function pointers to all 7 HAL traits:

| Trait          | Purpose                    | Driver Source                |
| -------------- | -------------------------- | ---------------------------- |
| `flash_hal`    | Flash read/write/erase     | `drivers/flash/...`          |
| `confirm_hal`  | Boot confirmation (RTC)    | `drivers/rtc/...`            |
| `wdt_hal`      | Watchdog timer             | `drivers/wdt/...`            |
| `reset_hal`    | Reset reason detection     | `drivers/clock/...`          |
| `console_hal`  | UART output                | `drivers/uart/...`           |
| `crypto_hal`   | Ed25519/SHA256             | Core SDK (Monocypher)        |
| `soc_hal`      | Chip-level clock/power     | Implemented inline           |

The struct fields reference the chip-local constants from `generated_boot_config.h` (e.g. `CHIP_FLASH_TOTAL_SIZE`, `CHIP_FLASH_ERASED_BYTE`).

---

### `chip_config.h`

A compatibility shim that `#include`s `generated_boot_config.h` and derives register offsets from the base addresses:

```c
#include "generated_boot_config.h"

/* Derived WDT registers for startup.c */
#define REG_TIMG0_WDT_CONFIG0   (CHIP_REG_TIMG0_WDT_BASE + 0x48U)
#define REG_TIMG0_WDT_FEED      (CHIP_REG_TIMG0_WDT_BASE + 0x60U)
#define REG_TIMG0_WDT_WPROTECT  (CHIP_REG_TIMG0_WDT_BASE + 0x64U)

/* Reset reason register (base + offset from constants) */
#define REG_RESET_CAUSE         (CHIP_REG_PMU_BASE + CHIP_RST_CAUSE_OFFSET)
```

> **Rule:** Never hardcode raw addresses here. Always derive from `CHIP_REG_*` bases. The bases come from `hardware.json`, so this file stays readable and portable.

---

### Linker Script (`<chip>_stage1.ld`)

Defines the MEMORY regions (IRAM, LP_RAM, Flash XIP) and SECTIONS layout. Must contain:

```ld
INCLUDE generated_memory.ld   /* Auto-generated partition addresses */
ENTRY(_start)

MEMORY
{
    iram (rwx)  : ORIGIN = 0x40800000, LENGTH = 0x7C000
    lp_ram (rw) : ORIGIN = 0x50000000, LENGTH = 0x4000
}
```

The `INCLUDE generated_memory.ld` line pulls in the auto-generated partition symbols from the Manifest Compiler. The MEMORY regions must match the chip's Technical Reference Manual.

---

## 2. Optional Files

### `template_device.toml`

A starter `device.toml` that `toob init --chip <name>` copies into a new project. Contains sensible defaults for partition sizes, boot config, and driver settings.

```toml
name = "MyDevice"
version = "v1.0.0"

[device]
chip = "esp32c6"

[build]
compiler = "riscv32-esp-elf"
core_sdk = "toob-boot"

[partitions]
stage0_size = 32768
stage1_size = 65536
app_size = 393216
```

### `mock_efuse.c`

A test-time mock for eFuse reads, used when the build is compiled with `-DTOOB_MOCK_EFUSES=1`. Provides deterministic identity keys for development without burning silicon. Listed in `sources.extra`.

---

## 3. Shared Dependencies

Your chip package will depend on artifacts outside `chips/<name>/`. These may already exist for your vendor/architecture.

### Architecture Package (`arch/<arch>/`)

Must contain:
- `arch_manifest.json` — Name, version, description
- `arch_timer.c` — Architecture-specific timer primitives
- `arch_trap.c` — Exception/interrupt vector setup
- `include/arch_riscv.h` (or equivalent) — Inline assembly helpers

If your architecture already exists (e.g. `arch/riscv32/`), you don't need to create one. Just reference it in your `chip_manifest.json`.

### SoC Vendor Header (`soc/<vendor>/include/`)

Shared across all chips from the same vendor. Provides:
- Register access macros (`REG_READ`, `REG_WRITE`, `REG_SET_BIT`)
- ROM function typedefs and typed pointer macros
- Error conversion utilities (`esp_rom_to_status()`)
- HAL API declarations

### Driver Packages (`drivers/<category>/<name>/`)

Each driver directory must contain:
- `driver_manifest.json` — Name, version, description
- One or more `.c` implementation files

```json
{
  "name": "esp_rom_spi",
  "author": "toob-core-team",
  "version": "1.0.0",
  "description": "ESP32 SPI Flash Driver via ROM pointers"
}
```

Driver categories must be registered in `driver_categories.json` at the registry root. Current categories: `clock`, `rtc`, `storage`, `crypto`, `console`, `power`, `wdt`, `bus`, `network`, `display`, `sensor`, `flash`, `uart`.

### Toolchain Package (`toolchains/<toolchain>/`)

Must contain:
- `toolchain_manifest.json` — Download URLs + SHA256 hashes per platform
- `toolchain.cmake` — CMake toolchain file for cross-compilation

```json
{
    "version": "1.0.0",
    "upstream_version": "13.2.0_20230928",
    "urls": {
        "linux_amd64":   "https://...",
        "darwin_amd64":  "https://...",
        "darwin_arm64":  "https://...",
        "windows_amd64": "https://..."
    },
    "sha256": {
        "linux_amd64":   "782feefe...",
        "darwin_amd64":  "e3b0c442...",
        "darwin_arm64":  "e3b0c442...",
        "windows_amd64": "1300a545..."
    }
}
```

> SHA256 hashes for `linux_amd64`, `darwin_amd64`, and `windows_amd64` are **mandatory**. The Registry Builder rejects toolchains without them.

---

## 4. Validation Pipeline

When you push changes to `main`, the following happens automatically:

1. **`build_registry.go`** scans all `chips/`, `arch/`, `drivers/`, `toolchains/` directories
2. For each chip, it validates:
   - All `sources.*` files exist on disk
   - The declared `arch` has a valid `arch_manifest.json`
   - The declared `compiler_prefix` maps to a registered toolchain
   - All driver paths resolve to real files
3. **`semver_calc.go`** computes version bumps based on content diffs
4. The aggregated `registry.json` is committed and tagged

### Fail-Fast Macro Validation

The Toob CLI runs an additional check at build time: every `CHIP_*` and `CHIP_REG_*` macro generated from `hardware.json` must be referenced by at least one `.c` file in the source tree. If you declare a constant that no driver uses, the build aborts. This prevents configuration drift.

---

## 5. Checklist

Use this as a PR self-review before submitting:

- [ ] `chip_manifest.json` has all required fields
- [ ] `hardware.json` has `flash`, `memory`, `registers` sections
- [ ] Every `constants` entry is consumed by at least one `.c` file
- [ ] No duplication between `flash.*` schema fields and `constants` block
- [ ] `startup.c` sterilizes all watchdogs and calls `boot_main()`
- [ ] `chip_platform.c` wires all 7 HAL traits
- [ ] `chip_config.h` derives all offsets from generated `CHIP_REG_*` bases
- [ ] Linker script includes `generated_memory.ld` and defines correct MEMORY regions
- [ ] All driver `.c` files listed in `sources.drivers` exist in the registry
- [ ] Architecture package exists at `arch/<arch>/`
- [ ] Toolchain package exists with valid SHA256 hashes
- [ ] Build passes: `toob build --native`

---

## 6. Contributing a Driver

Drivers are the reusable hardware implementations that chip packages wire into their `chip_platform.c`. A single driver can be shared across multiple chips (e.g. `esp_rom_spi` works for ESP32-C3, C6, and S3).

### Directory Structure

```
drivers/<category>/<driver_name>/
├── driver_manifest.json     ← Required
├── <implementation>.c       ← Required (one or more)
└── <implementation>.h       ← Optional (if cross-driver API needed)
```

### `driver_manifest.json` (Required)

```json
{
  "name": "esp_rom_spi",
  "author": "your-name",
  "version": "1.0.0",
  "description": "ESP32 SPI Flash Driver via ROM pointers"
}
```

| Field         | Required | Description                                    |
| ------------- | -------- | ---------------------------------------------- |
| `name`        | ✅        | Unique identifier. Must match the folder name. |
| `version`     | ✅        | SemVer. Bumped by the SemVer Calculator.       |
| `description` | ✅        | One-line summary.                              |
| `author`      | Optional | Contributor or team name.                      |

### Category Rules

The driver **must** be placed inside a valid category subfolder. The category is derived from the parent directory name and validated against `driver_categories.json`:

```
clock · rtc · storage · crypto · console · power · wdt · bus · network · display · sensor · flash · uart
```

If your driver doesn't fit any existing category, you must add the new category to `driver_categories.json` first.

### HAL Trait Contract

Every driver implements the functions for exactly one HAL trait defined in `boot_hal.h`. The chip's `chip_platform.c` then wires your functions into the corresponding trait struct. Here is the mapping:

| Category | HAL Trait        | Required Functions                                                           |
| -------- | ---------------- | ---------------------------------------------------------------------------- |
| `flash`  | `flash_hal_t`    | `init`, `deinit`, `read`, `write`, `erase_sector`, `get_sector_size`, `get_last_vendor_error` |
| `wdt`    | `wdt_hal_t`      | `init(timeout_ms)`, `deinit`, `kick`, `suspend_for_critical_section`, `resume` |
| `uart`   | `console_hal_t`  | `init(baudrate)`, `deinit`, `putchar`, `getchar`, `flush`                    |
| `clock`  | `clock_hal_t`    | `init`, `deinit`, `get_tick_ms`, `delay_ms`, `get_reset_reason`              |
| `rtc`    | `confirm_hal_t`  | `init`, `deinit`, `check_ok(nonce)`, `clear`                                 |
| `crypto` | `crypto_hal_t`   | `init`, `deinit`, `hash_*`, `verify_ed25519`, `random`, `read_pubkey`, ...   |

> **Rule:** All `init()` functions return `boot_status_t`. All `deinit()` functions must be idempotent and void. These are enforced by the ABI version check (`TOOB_HAL_ABI_V2`).

### Consuming Generated Constants

Drivers must consume `CHIP_*` and `CHIP_REG_*` macros from `generated_boot_config.h` (via `chip_config.h` or `esp_common.h`), never hardcode hardware values. The Manifest Compiler's fail-fast validation will reject any build where a declared constant has zero consumers.

### Wiring Your Driver to a Chip

After creating the driver, reference it in the chip's `chip_manifest.json`:

```json
"sources": {
    "drivers": [
        "drivers/flash/my_new_driver/flash.c"
    ]
}
```

Then wire the function pointers in the chip's `chip_platform.c`.

### Driver Checklist

- [ ] `driver_manifest.json` has `name`, `version`, `description`
- [ ] Driver is in a valid category folder
- [ ] Implementation follows the HAL trait signature from `boot_hal.h`
- [ ] Uses only `CHIP_*`/`CHIP_REG_*` macros — no hardcoded addresses
- [ ] All functions return `boot_status_t` or `void` per the trait contract
- [ ] `deinit()` is idempotent

---

## 7. Contributing a Toolchain

Toolchains define how Toob auto-provisions cross-compilers. If your chip uses a compiler that doesn't exist in the registry yet (e.g. `arm-none-eabi-` for Cortex-M chips), you need to add one.

### Directory Structure

```
toolchains/<toolchain_name>/
├── toolchain_manifest.json  ← Required
└── toolchain.cmake          ← Required
```

### `toolchain_manifest.json` (Required)

Declares download URLs and SHA256 checksums for each platform:

```json
{
    "version": "1.0.0",
    "upstream_version": "13.2.0_20230928",
    "urls": {
        "linux_amd64":   "https://github.com/.../gcc-arm-none-eabi-...-x86_64-linux.tar.xz",
        "darwin_amd64":  "https://github.com/.../gcc-arm-none-eabi-...-x86_64-apple-darwin.tar.xz",
        "darwin_arm64":  "https://github.com/.../gcc-arm-none-eabi-...-aarch64-apple-darwin.tar.xz",
        "windows_amd64": "https://github.com/.../gcc-arm-none-eabi-...-x86_64-w64-mingw32.zip"
    },
    "sha256": {
        "linux_amd64":   "a1b2c3...",
        "darwin_amd64":  "d4e5f6...",
        "darwin_arm64":  "789abc...",
        "windows_amd64": "def012..."
    }
}
```

| Field              | Required | Notes                                                              |
| ------------------ | -------- | ------------------------------------------------------------------ |
| `version`          | ✅        | Registry-internal version. Bump when URLs/hashes change.           |
| `upstream_version` | ✅        | The upstream compiler release string (for `--version` validation). |
| `urls`             | ✅        | Download URLs per `<os>_<arch>` key.                               |
| `sha256`           | ✅        | 64-character hex hashes. **Mandatory** for `linux_amd64`, `darwin_amd64`, `windows_amd64`. |

> The Registry Builder validates that every SHA256 hash is exactly 64 hex characters. Empty or short hashes cause a `FATAL` rejection.

### `toolchain.cmake` (Required)

A CMake toolchain file that configures the cross-compiler for bare-metal builds. It must:

1. Set `CMAKE_SYSTEM_NAME Generic` and `CMAKE_SYSTEM_PROCESSOR`
2. Define `CMAKE_C_COMPILER` using the `TOOLCHAIN_PREFIX` and optional `TOOLCHAIN_BIN_DIR` (hermetic mode)
3. Bypass CMake's compiler test (`CMAKE_C_COMPILER_WORKS TRUE`)
4. Define architecture-specific flags (`-march`, `-mabi`, etc.) per chip variant
5. Set bare-metal linker flags (`--specs=nano.specs`, `--specs=nosys.specs`, `-nostartfiles`, `-Wl,--gc-sections`)

The existing `riscv32-esp-elf/toolchain.cmake` is a good reference — it supports hermetic paths (CLI-injected), per-chip ISA flags, and Windows/Unix duality.

### How Auto-Provisioning Works

When a user runs `toob build --native` and no toolchain is found locally:

1. The CLI reads `compiler_prefix` from `chip_manifest.json` (e.g. `riscv32-esp-elf-`)
2. It strips the trailing `-` to get the toolchain name (`riscv32-esp-elf`)
3. It looks up the matching entry in `registry.json` → `toolchains`
4. It downloads the archive for the current OS/arch and verifies the SHA256
5. It extracts to `~/.toob/toolchains/<name>/<upstream_version>/`

### Toolchain Checklist

- [ ] `toolchain_manifest.json` has `version`, `upstream_version`, `urls`, `sha256`
- [ ] SHA256 hashes present for `linux_amd64`, `darwin_amd64`, `windows_amd64`
- [ ] All download URLs are stable (pinned releases, not `latest` redirects)
- [ ] `toolchain.cmake` sets `CMAKE_SYSTEM_NAME Generic`
- [ ] `toolchain.cmake` bypasses compiler test (`CMAKE_C_COMPILER_WORKS TRUE`)
- [ ] Architecture flags are parameterized by `TOOB_CHIP`
- [ ] Hermetic `TOOLCHAIN_BIN_DIR` path is supported

---

## 8. Contributing an Architecture Package

Architecture packages provide ISA-level abstractions shared across all chips of the same architecture. For example, `arch/riscv32/` is used by ESP32-C3, ESP32-C6, and any future RISC-V chip.

### Directory Structure

```
arch/<arch_name>/
├── arch_manifest.json        ← Required
├── arch_timer.c              ← Required
├── arch_trap.c               ← Required
└── include/
    └── arch_<name>.h         ← Required
```

### `arch_manifest.json` (Required)

```json
{
  "name": "riscv32",
  "version": "1.0.0",
  "description": "RISC-V 32-bit Architecture Support"
}
```

| Field         | Required | Description                              |
| ------------- | -------- | ---------------------------------------- |
| `name`        | ✅        | Must match the directory name.           |
| `version`     | ✅        | SemVer.                                  |
| `description` | ✅        | One-line summary.                        |

### Required Implementations

#### `arch_timer.c`

Provides the timing primitives that `clock_hal_t` delegates to. Must implement:

```c
void     arch_<name>_timer_init(uint32_t timer_base, uint32_t cpu_freq_hz);
uint32_t arch_<name>_get_tick_ms(void);
void     arch_<name>_delay_ms(uint32_t ms);
```

The chip's `chip_platform.c` calls `timer_init()` with the chip-specific base address and frequency from `hardware.json`. The timer implementation must be hardware-accurate and ISR-safe.

#### `arch_trap.c`

Installs the architecture-specific exception/interrupt vector and dispatches to Toob's `toob_ecc_trap()` on hardware faults. Must implement:

```c
void arch_<name>_trap_entry(void);
```

For RISC-V, this is the `mtvec` handler. For ARM, it would be the `HardFault_Handler` + vector table. The contract from `boot_hal.h` requires that ECC/bit-rot exceptions are caught and forwarded to `toob_ecc_trap()` rather than causing an infinite hang.

#### `include/arch_<name>.h`

Public header providing:

- **CSR / Register access macros** (e.g. `csr_read`, `csr_write` for RISC-V)
- **Interrupt control** (`disable_interrupts`, `enable_interrupts`)
- **Timer API declarations** (the functions implemented in `arch_timer.c`)
- **Trap entry declaration** (the function implemented in `arch_trap.c`)

This header is included by `startup.c` and `chip_platform.c` of every chip that declares this architecture.

### Cross-Chip Portability Rules

Architecture code must **never** reference chip-specific constants directly. Instead:

- Timer base addresses are passed as function parameters (not `#define`d)
- CPU frequencies are passed at init time
- Any chip-specific quirks belong in the chip's `startup.c`, not in `arch_timer.c`

### Architecture Checklist

- [ ] `arch_manifest.json` has `name`, `version`, `description`
- [ ] `arch_timer.c` implements `timer_init`, `get_tick_ms`, `delay_ms`
- [ ] `arch_trap.c` implements the exception vector and dispatches to `toob_ecc_trap()`
- [ ] `include/arch_<name>.h` declares all public APIs
- [ ] No chip-specific `#define`s or `#include`s in architecture code
- [ ] At least one chip's `chip_manifest.json` references this architecture
