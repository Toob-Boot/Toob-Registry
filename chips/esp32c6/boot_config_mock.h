/**
 * @file boot_config_mock.h
 * @brief ESP32-C6 Override — Redirects to the real boot_config.h
 *
 * The core source files include "boot_config_mock.h" directly. On real
 * hardware targets, this file acts as a redirect to the chip-specific
 * boot_config.h which provides the actual flash partition layout.
 *
 * This file is placed in hal/chips/esp32c6/ so the include path
 * (hal/chips/${TOOB_CHIP} listed before core/include) ensures the
 * compiler finds THIS file instead of the sandbox mock.
 */

#ifndef BOOT_CONFIG_MOCK_H
#define BOOT_CONFIG_MOCK_H

#include "boot_config.h"

#endif /* BOOT_CONFIG_MOCK_H */
