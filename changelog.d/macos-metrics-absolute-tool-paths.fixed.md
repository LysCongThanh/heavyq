**macOS RAM/load gate under cron.** The local macOS sampler now calls `/usr/sbin/sysctl` and `/usr/bin/vm_stat` by
absolute path (falling back to `PATH` only when they are missing). Under a stripped `PATH` such as cron's
`/usr/bin:/bin`, `sysctl` was not found, the Mac read as "resources unknown" and the gate silently enforced slots only,
admitting heavy jobs with RAM exhausted or load high.
