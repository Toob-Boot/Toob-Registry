# ==============================================================================
# Toolchain: RISC-V 32-bit (ESP32-C3, ESP32-C6, ESP32-P4)
# (Trigger Comment)
# ==============================================================================

set(CMAKE_SYSTEM_NAME Generic)
set(CMAKE_SYSTEM_PROCESSOR riscv)

# ------------------------------------------------------------------------------
# 1. Compiler Base Setup & Prefixing
# ------------------------------------------------------------------------------
set(TOOLCHAIN_PREFIX "riscv32-esp-elf-" CACHE STRING "RISC-V Toolchain Prefix")

# Hermetic Builds: Wenn die CLI den Pfad übergibt, zwingen wir CMake dazu, 
# diese absoluten Pfade zu nutzen. Kein PATH-Polluting!
if(TOOLCHAIN_BIN_DIR)
    if(CMAKE_HOST_WIN32)
        set(CMAKE_C_COMPILER "${TOOLCHAIN_BIN_DIR}/${TOOLCHAIN_PREFIX}gcc.exe")
        set(CMAKE_ASM_COMPILER "${CMAKE_C_COMPILER}")
        find_program(CMAKE_OBJCOPY NAMES ${TOOLCHAIN_PREFIX}objcopy.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_OBJDUMP NAMES ${TOOLCHAIN_PREFIX}objdump.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_SIZE NAMES ${TOOLCHAIN_PREFIX}size.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_AR NAMES ${TOOLCHAIN_PREFIX}ar.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_RANLIB NAMES ${TOOLCHAIN_PREFIX}ranlib.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_STRIP NAMES ${TOOLCHAIN_PREFIX}strip.exe PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
    else()
        set(CMAKE_C_COMPILER "${TOOLCHAIN_BIN_DIR}/${TOOLCHAIN_PREFIX}gcc")
        set(CMAKE_ASM_COMPILER "${CMAKE_C_COMPILER}")
        find_program(CMAKE_OBJCOPY NAMES ${TOOLCHAIN_PREFIX}objcopy PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_OBJDUMP NAMES ${TOOLCHAIN_PREFIX}objdump PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_SIZE NAMES ${TOOLCHAIN_PREFIX}size PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_AR NAMES ${TOOLCHAIN_PREFIX}ar PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_RANLIB NAMES ${TOOLCHAIN_PREFIX}ranlib PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
        find_program(CMAKE_STRIP NAMES ${TOOLCHAIN_PREFIX}strip PATHS ${TOOLCHAIN_BIN_DIR} NO_DEFAULT_PATH)
    endif()
else()
    if(CMAKE_HOST_WIN32)
        set(CMAKE_C_COMPILER ${TOOLCHAIN_PREFIX}gcc.exe)
        set(CMAKE_ASM_COMPILER ${CMAKE_C_COMPILER})
    else()
        set(CMAKE_C_COMPILER ${TOOLCHAIN_PREFIX}gcc)
        set(CMAKE_ASM_COMPILER ${CMAKE_C_COMPILER})
    endif()
    find_program(CMAKE_OBJCOPY NAMES ${TOOLCHAIN_PREFIX}objcopy)
    find_program(CMAKE_OBJDUMP NAMES ${TOOLCHAIN_PREFIX}objdump)
    find_program(CMAKE_SIZE NAMES ${TOOLCHAIN_PREFIX}size)
    find_program(CMAKE_AR NAMES ${TOOLCHAIN_PREFIX}ar)
    find_program(CMAKE_RANLIB NAMES ${TOOLCHAIN_PREFIX}ranlib)
    find_program(CMAKE_STRIP NAMES ${TOOLCHAIN_PREFIX}strip)
endif()

# ------------------------------------------------------------------------------
# 2. Compiler Test Bypass (Kritisch für Bare-Metal & ESP-IDF Toolchains)
# ------------------------------------------------------------------------------
set(CMAKE_TRY_COMPILE_TARGET_TYPE STATIC_LIBRARY)

# Bypass Compiler Check
# Da wir Architektur-Flags erst in CMAKE_C_FLAGS_INIT injizieren, schlägt CMake's 
# initialer Compiler Test mit dem Espressif Default oft fehl, was den Lauf abbricht.
set(CMAKE_C_COMPILER_WORKS TRUE)
set(CMAKE_ASM_COMPILER_WORKS TRUE)
set(CMAKE_CXX_COMPILER_WORKS TRUE)
set(CMAKE_C_COMPILER_FORCED TRUE)
set(CMAKE_CXX_COMPILER_FORCED TRUE)

# ------------------------------------------------------------------------------
# 3. Hardware-Spezifische CPU-Flags
# ------------------------------------------------------------------------------
set(CHIP_ARCH_FLAGS "")

if(TOOB_CHIP STREQUAL "esp32c6")
    # ESP32-C6: integer multiply, atomics, compressed instructions.
    set(CHIP_ARCH_FLAGS "-march=rv32imac_zicsr -mabi=ilp32")
elseif(TOOB_CHIP STREQUAL "esp32c3")
    set(CHIP_ARCH_FLAGS "-march=rv32imc -mabi=ilp32")
elseif(TOOB_CHIP STREQUAL "esp32p4")
    set(CHIP_ARCH_FLAGS "-march=rv32imafdc -mabi=ilp32f")
else()
    message(WARNING "Unknown ESP32 RISC-V Chip: ${TOOB_CHIP}. Using default rv32imac.")
    set(CHIP_ARCH_FLAGS "-march=rv32imac -mabi=ilp32")
endif()

# ------------------------------------------------------------------------------
# 4. Architektur-unabhängige Bare-Metal Compiler/Linker Flags
# ------------------------------------------------------------------------------
set(TOOB_RISCV_C_FLAGS 
    "${CHIP_ARCH_FLAGS}"
    "-ffunction-sections" 
    "-fdata-sections"     
    "-fno-common"         
    "-fno-builtin"        
)

# Newlib/Nano Specs zwingen den Compiler zu winzigen C-Library Footprints
# und blockieren fette Malloc-Syscalls.
set(TOOB_RISCV_LINKER_FLAGS 
    "${CHIP_ARCH_FLAGS}"
    "--specs=nano.specs" 
    "--specs=nosys.specs"
    "-Wl,--gc-sections"
    "-nostartfiles"
)

# Listen zu CMake-Strings formatieren
string(REPLACE ";" " " TOOB_RISCV_C_FLAGS_STR "${TOOB_RISCV_C_FLAGS}")
string(REPLACE ";" " " TOOB_RISCV_LINKER_FLAGS_STR "${TOOB_RISCV_LINKER_FLAGS}")

# Initiale Flags an CMake übergeben
set(CMAKE_C_FLAGS_INIT "${TOOB_RISCV_C_FLAGS_STR}")
set(CMAKE_ASM_FLAGS_INIT "${TOOB_RISCV_C_FLAGS_STR}")
set(CMAKE_EXE_LINKER_FLAGS_INIT "${TOOB_RISCV_LINKER_FLAGS_STR}")
