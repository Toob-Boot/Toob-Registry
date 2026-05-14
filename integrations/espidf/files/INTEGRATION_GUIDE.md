# Toob ESP-IDF Integration
1. Use `partitions.csv` as your custom partition table in sdkconfig.
2. Add `toob_integration/toob_hooks.c` to your main component SRCS.
3. Call `TOOB_OS_INIT_OR_PANIC();` as the very first line in your app_main().
