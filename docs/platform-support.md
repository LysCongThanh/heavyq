# Platform support

Linux is the primary target. CI runs the whole suite there, plus a **macOS** job (the metrics and process
tests, `status` and `cancel`) on `macos-latest`. heavyq never uses cgo, so macOS is measured with the tools
every Mac has:

| | Linux | macOS |
|---|---|---|
| RAM / load gate | `/proc/meminfo`, `/proc/loadavg` | `sysctl -n hw.memsize hw.ncpu vm.loadavg` and `vm_stat` |
| Per-lane CPU, "outside the queue" | `/proc/<pid>/stat` | `ps -axo pid=,ppid=,rss=,time=,comm=`, sampled twice |
| Process-tree cancel | child subreaper: orphans re-parent to heavyq | walk of the `ps` ppid map; no subreaper |
| Process identity (pid reuse), stopped waiters | `/proc/<pid>/stat` start time and state | `ps -o pid=,stat=,lstart= -p <all ticket pids>` with `TZ=UTC0`, one run per prune |
| Remote machines | yes | no (remotes must be Linux; a Mac can still *use* them) |

On macOS "available memory" is `free + inactive + purgeable + speculative` pages times the page size, capped at
installed RAM. `Pages free` alone sits near zero on a healthy Mac (the kernel keeps spare RAM as file cache), so
gating on it would never admit anything. The other three are memory the system hands back on demand; active,
wired and compressed pages hold live data and are not counted. It is an estimate, slightly optimistic.

Two macOS limits. `ps` prints CPU time with centisecond precision, so CPU% is as exact as on Linux, but each
sample costs one `ps` run (tens of ms). And with no subreaper, a cancel signals every descendant that is still
attached to its parent; a process whose parent already exited (a daemon the command started, or an orphan
left by a command that died on SIGTERM) re-parents to `launchd` and can be missed. If a command fails to run,
the metrics are reported unknown and the gate falls back to slots only, as before.
