# Toob Zephyr Integration
1. Append the contents of `app.overlay` to your device tree overlay.
2. Add `toob_integration/toob_hooks.c` to your CMake target sources.
3. Call `TOOB_OS_INIT_OR_PANIC();` as the very first line in your main().
