# ==============================================================================
# Toolchain: RISC-V 32-bit (ESP32-C3, ESP32-C6, GD32V)
# 
# Relevant Specs: 
# - docs/structure_plan.md (Arch-Vendor-Chip Mapping für riscv32)
# - docs/provisioning_guide.md (Hardware Target Constraints)
# ==============================================================================

set(CMAKE_SYSTEM_NAME Generic)
set(CMAKE_SYSTEM_PROCESSOR riscv)

# ------------------------------------------------------------------------------
# 1. Compiler Base Setup & Prefixing
# ------------------------------------------------------------------------------
# HINWEIS: Espressif liefert oft `riscv32-esp-elf-` via ESP-IDF.
# Für generisches RISC-V Bare-Metal nutzen wir als Default `riscv32-unknown-elf-`.
# Das Prefix kann von der CLI oder device.toml per -DTOOLCHAIN_PREFIX überschrieben werden.
set(TOOLCHAIN_PREFIX "riscv32-unknown-elf-" CACHE STRING "RISC-V Toolchain Prefix")

set(CMAKE_C_COMPILER ${TOOLCHAIN_PREFIX}gcc)
set(CMAKE_ASM_COMPILER ${CMAKE_C_COMPILER})

# Utilities für Binary-Weaving und Analyse (z.B. OSV / P10 Checks)
find_program(CMAKE_OBJCOPY NAMES ${TOOLCHAIN_PREFIX}objcopy)
find_program(CMAKE_OBJDUMP NAMES ${TOOLCHAIN_PREFIX}objdump)
find_program(CMAKE_SIZE NAMES ${TOOLCHAIN_PREFIX}size)

# BUGFIX/GAP: Da wir CMAKE_TRY_COMPILE_TARGET_TYPE auf STATIC_LIBRARY setzen,
# MÜSSEN wir zwingend den Archiver explizit setzen, da sonst der Host-Archiver crasht.
find_program(CMAKE_AR NAMES ${TOOLCHAIN_PREFIX}ar)
find_program(CMAKE_RANLIB NAMES ${TOOLCHAIN_PREFIX}ranlib)
find_program(CMAKE_STRIP NAMES ${TOOLCHAIN_PREFIX}strip)

# ------------------------------------------------------------------------------
# 2. Compiler Test Bypass (Kritisch für Bare-Metal)
# ------------------------------------------------------------------------------
# Verhindert, dass CMake beim initialen Compiler-Check versucht, ohne 
# fertiges RISC-V Linker-Script ein komplettes .out Executable zu bauen (was fehlschlägt).
set(CMAKE_TRY_COMPILE_TARGET_TYPE STATIC_LIBRARY)

# ------------------------------------------------------------------------------
# 3. Architektur-unabhängige Bare-Metal Compiler/Linker Flags
# ------------------------------------------------------------------------------
# NOTE: Architektur-Flags wie "-march=rv32imc" und "-mabi=ilp32" DÜRFEN NICHT hier
# konfiguriert werden! Sie müssen zwingend in `cmake/toob_hal.cmake` injiziert 
# werden, da ein ESP32-C6 z.B. andere Extensions nutzt als ein eckiger GD32V.

# NOTE: Code-Model -mcmodel=medany MUSS zwingend evaluiert werden! 
# ESP32-Chips (wie der C3) mappen Flash und RAM oft oberhalb von 2GB (z.B. 0x4000_0000). 
# Das standardmäßig genutzte `medlow` Model crasht den Linker bei solchen Adressen!
# Dies muss architekturspezifisch in der HAL gesetzt werden.

set(TOOB_RISCV_C_FLAGS 
    "-ffunction-sections" 
    "-fdata-sections"     
    "-fno-common"         
    "-fno-builtin"        
)

# Newlib/Nano Specs zwingen den Compiler zu winzigen C-Library Footprints
# und blockieren fette Malloc-Syscalls.
set(TOOB_RISCV_LINKER_FLAGS 
    "--specs=nano.specs" 
    "--specs=nosys.specs"
    "-Wl,--gc-sections"
    "-nostartfiles"
)

# Flags global an die CMake Init Variablen hängen
string(REPLACE ";" " " TOOB_RISCV_C_FLAGS_STR "${TOOB_RISCV_C_FLAGS}")
string(REPLACE ";" " " TOOB_RISCV_LINKER_FLAGS_STR "${TOOB_RISCV_LINKER_FLAGS}")

# Initiale Flags an CMake übergeben
set(CMAKE_C_FLAGS_INIT "${TOOB_RISCV_C_FLAGS_STR}")
set(CMAKE_ASM_FLAGS_INIT "${TOOB_RISCV_C_FLAGS_STR}")
set(CMAKE_EXE_LINKER_FLAGS_INIT "${TOOB_RISCV_LINKER_FLAGS_STR}")
