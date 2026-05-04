/**
 * @file vendor_console.c
 * @brief ESP UART0 Console HAL via Direct Register Writes
 *
 * Implements console_hal_t for boot-time debug logging over UART0.
 * Uses bare-metal register access — no ESP-IDF UART driver.
 *
 * REFERENCED SPECIFICATIONS:
 * 1. docs/hals.md Section 6 (console_hal_t)
 *    - putchar: blocking FIFO write.
 *    - getchar: non-blocking polling with timeout.
 *    - "Zwingend non-blocking polling fuer WDT-Sicherheit."
 * 2. chip_config.h
 *    - CHIP_UART0_BASE, CHIP_UART_SCLK_FREQ, CHIP_UART_TX_FIFO_SIZE
 *    - CHIP_CPU_FREQ_HZ (for spin-loop timing).
 *
 * UART register layout is shared across all ESP variants (ESP32/S2/S3/C3/C6/H2).
 * Only base address and clock frequency differ — injected via chip_config.h.
 */

#include "esp_common.h"
#include "chip_config.h"

#include <stdint.h>

/* UART0 register offsets */
#define UART_FIFO_OFF          0x00U
#define UART_STATUS_OFF        0x1CU
#define UART_CLKDIV_SYNC_OFF   0x14U
#define UART_CONF0_SYNC_OFF    0x20U
#define UART_CLK_CONF_OFF      0x88U
#define UART_REG_UPDATE_OFF    0x98U
#define UART_ID_OFF            0x9CU

/* STATUS register bit masks */
#define UART_TXFIFO_CNT_MASK   (0xFFU << 16)
#define UART_TXFIFO_CNT_SHIFT  16U
#define UART_RXFIFO_CNT_MASK   0xFFU

/* CONF0_SYNC register bit masks */
#define UART_RXFIFO_RST        (1U << 22)
#define UART_TXFIFO_RST        (1U << 23)
#define UART_MEM_CLK_EN        (1U << 20)

/* TX FIFO capacity — chip-specific (from chip_config.h) */
#define UART_TX_FIFO_SIZE      CHIP_UART_TX_FIFO_SIZE

static uint32_t s_uart_base;

static inline uint32_t uart_reg_read(uint32_t off)
{
    return REG_READ(s_uart_base + off);
}

static inline void uart_reg_write(uint32_t off, uint32_t val)
{
    REG_WRITE(s_uart_base + off, val);
}

/**
 * @brief Synchronize UART register writes.
 *
 * The ESP32-C6 UART has a register synchronization mechanism. After
 * changing configuration registers, bit 0 of REG_UPDATE must be set
 * and then polled until the hardware clears it.
 */
static void uart_sync_regs(void)
{
    uart_reg_write(UART_REG_UPDATE_OFF, 1U);

    for (uint32_t guard = 0; guard < 10000U; guard++) {
        if ((uart_reg_read(UART_REG_UPDATE_OFF) & 1U) == 0U) {
            return;
        }
    }
}

boot_status_t esp_uart_init(uint32_t baudrate)
{
    s_uart_base = CHIP_UART0_BASE;

    if (baudrate == 0U) {
        return BOOT_ERR_INVALID_ARG;
    }

    /* Enable UART clock */
    uart_reg_write(UART_CLK_CONF_OFF, (1U << 25) | (1U << 24));

    /* Reset FIFOs */
    uint32_t conf0 = uart_reg_read(UART_CONF0_SYNC_OFF);
    uart_reg_write(UART_CONF0_SYNC_OFF, conf0 | UART_RXFIFO_RST | UART_TXFIFO_RST | UART_MEM_CLK_EN);
    uart_sync_regs();
    uart_reg_write(UART_CONF0_SYNC_OFF, (conf0 | UART_MEM_CLK_EN) & ~(UART_RXFIFO_RST | UART_TXFIFO_RST));
    uart_sync_regs();

    /* Set baudrate: divider = SCLK / baudrate */
    uint32_t divider = CHIP_UART_SCLK_FREQ / baudrate;
    uint32_t frag = ((CHIP_UART_SCLK_FREQ % baudrate) * 16U) / baudrate;
    uart_reg_write(UART_CLKDIV_SYNC_OFF, (divider & 0xFFFU) | ((frag & 0xFU) << 20));
    uart_sync_regs();

    /* 8N1 configuration: 8 data bits (0x3), 1 stop bit (0x1), no parity */
    conf0 = uart_reg_read(UART_CONF0_SYNC_OFF);
    conf0 &= ~(0x3U << 2);  /* bit_num field */
    conf0 |= (0x3U << 2);   /* 8 data bits */
    conf0 &= ~(0x3U << 4);  /* stop_bit_num field */
    conf0 |= (0x1U << 4);   /* 1 stop bit */
    conf0 &= ~(0x1U << 1);  /* parity_en = 0 */
    conf0 |= UART_MEM_CLK_EN;
    uart_reg_write(UART_CONF0_SYNC_OFF, conf0);
    uart_sync_regs();

    return BOOT_OK;
}

void esp_uart_deinit(void)
{
    esp_uart_flush();
    /* Leave UART configured for OS handoff (serial rescue may need it) */
}

/**
 * @brief Blocking putchar with FIFO space check.
 *
 * Waits until there is space in the TX FIFO, then writes the byte.
 * Bounded spin-wait for P10 compliance (max 100000 iterations).
 */
void esp_uart_putchar(char c)
{
    for (uint32_t guard = 0; guard < 100000U; guard++) {
        uint32_t status = uart_reg_read(UART_STATUS_OFF);
        uint32_t tx_cnt = (status & UART_TXFIFO_CNT_MASK) >> UART_TXFIFO_CNT_SHIFT;

        if (tx_cnt < UART_TX_FIFO_SIZE) {
            uart_reg_write(UART_FIFO_OFF, (uint32_t)(uint8_t)c);
            return;
        }
    }
}

/**
 * @brief Non-blocking getchar with timeout.
 *
 * Polls the RX FIFO count and reads if data is available.
 * Uses arch timer for timeout (requires clock_hal to be initialized first).
 * Falls back to a spin counter if timer is not yet available.
 */
boot_status_t esp_uart_getchar(uint8_t *out, uint32_t timeout_ms)
{
    if (out == NULL) {
        return BOOT_ERR_INVALID_ARG;
    }

    /*
     * Simple spin-based timeout using CHIP_CPU_FREQ_HZ.
     * For boot-time serial rescue, precision is not critical.
     */
    uint32_t iterations = timeout_ms * (CHIP_CPU_FREQ_HZ / 1000U);
    if (iterations == 0U) {
        iterations = 1U;
    }

    for (uint32_t i = 0; i < iterations; i++) {
        uint32_t status = uart_reg_read(UART_STATUS_OFF);
        uint32_t rx_cnt = status & UART_RXFIFO_CNT_MASK;

        if (rx_cnt > 0U) {
            *out = (uint8_t)(uart_reg_read(UART_FIFO_OFF) & 0xFFU);
            return BOOT_OK;
        }
    }

    return BOOT_ERR_TIMEOUT;
}

void esp_uart_flush(void)
{
    for (uint32_t guard = 0; guard < 500000U; guard++) {
        uint32_t status = uart_reg_read(UART_STATUS_OFF);
        uint32_t tx_cnt = (status & UART_TXFIFO_CNT_MASK) >> UART_TXFIFO_CNT_SHIFT;

        if (tx_cnt == 0U) {
            return;
        }
    }
}
