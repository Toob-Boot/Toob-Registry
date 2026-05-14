# Toob Baremetal Integration
1. Compile and link `toob_integration/toob_hooks.c` with your project.
2. Include the Toob SDK headers.
3. Call `TOOB_OS_INIT_OR_PANIC();` as the very first line in your main().
