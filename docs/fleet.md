# Fleet: observe every machine and every queue

`heavyq fleet` is the observation layer of a fleet: every machine you can reach and every queue that runs
heavy work there, heavyq's own **and other queues** heavyq does not run. It is read-only and daemonless. It
never schedules, never takes a slot, never prunes a ticket; it measures, reads and reports.

```bash
heavyq fleet                         # a short terminal report
heavyq fleet --since 6h              # the history window (default 24h)
heavyq fleet --json                  # the whole report, schema/fleet.schema.json
heavyq fleet --md ~/reports/fleet.md # a markdown report for other agents, written atomically; stdout stays quiet
heavyq fleet --agent                 # a digest under 4 KB (automatic in agent mode)
```

Exit status: 0 when the report is out (an unreachable or overloaded machine is data, not an error), 2 for a
usage or config error (nothing is probed), 1 when `--md` cannot be written.

## What it reports

**Per machine** (this machine, every `[[remote]]`, every `observe_only` remote), probed in parallel over its
transport, each within `--timeout` (default 10s). A machine that does not answer in time is shown as
`unreachable` with the reason; it never holds the report.

**A slow machine degrades, it is not unreachable.** The probe measures cheapest first and sends each part as
soon as it has it: the machine's numbers (cores, load, RAM, pressure), then slot states, then the expensive
part (the CPU sample of every process, the busiest command lines, heavyq job attribution). It starts no
expensive step once half the timeout is spent, and the transport is ended at the timeout; a machine that sent
its numbers by then is shown with them and with `partial: <what is missing> (<why>)` in the terminal report,
the markdown report (state column) and the digest, and `"partial"` in JSON. Only a machine that sent nothing
usable within the timeout is `unreachable`. On a loaded machine starting a process can take half a second, so
the probe starts a fixed handful of them, never one per process: `/proc/<pid>/stat` is read by one `cat` per
sample and parsed by one `awk`, command lines are read by the shell itself, only for the busiest, and given up on when they do not
return within the budget (such a read waits for the process's memory map lock and can hang for minutes).

**Zombies are not processes to count.** A task that exited and was not reaped (state `Z`, often by the thousand
in a container without an init) uses no CPU and no memory: it is left out of every group, count and
attribution, on `/proc` and on `ps`. Stopped and frozen tasks (`kill -STOP`, a `docker pause`d container) keep
their RAM, so they stay in their group at 0% CPU; their command lines are never read (nor those of tasks in
`D` state, which can hold the lock such a read waits on).

| Field | Meaning |
|---|---|
| cores, load1, load/cores | from `/proc` (Linux) or `sysctl` (macOS) |
| free / total RAM | `MemAvailable` / `MemTotal` (macOS: the scheduler's own formula) |
| memory pressure | Linux `/proc/pressure/memory` `full avg10` (and `cpu some avg10` in JSON), when the kernel has it |
| OVERLOAD | the rule below, with the reasons |
| top | the busiest process groups: command name, processes, threads, %CPU, RSS, and **who they belong to**: a queue and lane, or `outside` |
| outside every queue | RSS of the processes no queue owns (what exhausts RAM behind the queues' backs) |
| slots | busy/total slots by owner: `heavyq` and each foreign queue |
| worker cap | the suggested number of workers per heavy run, with the arithmetic shown |

%CPU on Linux is measured over a 1 second sample of `/proc/<pid>/stat` (one core = 100%), not `ps`'s CPU time
over a process's whole life. The sample is timed by `/proc/uptime`, read together with the stat files, and each
process is measured over the span between its own two reads: on a loaded machine a read of the process table
starts seconds after the 1 second sleep ends and itself takes a second or two, so the sample is often longer
than a second. On macOS it is `ps`'s decaying average. Command lines go through the same
redaction as everything else heavyq prints.

**Attribution.** A process belongs to the nearest of its ancestors that is known to a queue:

- a ticket's own process, on the machine whose ticket directory it is (heavyq's queue dir, a foreign queue's
  tickets dir), so a local run's whole process tree is its lane's;
- the holder of a slot lock (Linux reads the holder from `/proc/locks`, without taking the lock), named after
  the running ticket that says it holds that slot on that machine;
- a process working in `<work_dir>/wt/` of a heavyq remote: heavyq.

**Per queue and per machine**, over the window: runs, run-hours, wait p50/p90/p95, run p50, runs per hour, and
red runs split into **infra** and **code**, as shares of all runs. **Live**: every running and waiting ticket of
every queue, longest wait first, with lane names. Tickets whose submitting process is gone are counted as
`stale`, not shown as waiting.

## The rules

**Overload.** A machine is OVERLOADED when any of these holds:

- `load1 / cores >= 1.5`: half again as many runnable threads as cores, every job at two thirds of its speed
  or worse;
- free RAM below 10% of the total (below 1024 MB when the total is unknown): the next job swaps or meets the
  OOM killer;
- memory pressure `full avg10 >= 10%`: all tasks stalled on memory that share of the last 10 seconds.

CPU pressure is shown but is not a trigger on its own: it is already in the load.

**Infra or code.** A red run (exit not 0) is **infra** when its exit is 127 (command not found), 137 (killed:
the OOM killer, `kill -9`), 255 (the ssh connection dropped), or one of the queue's own `infra_exits`. Every
other red run is **code**. For heavyq's own runs, 77 (remote preparation failed) and cancelled or
interrupted runs are infra; runs that gave up waiting (75, 76) or were served from the cache are not runs.

**Worker cap.** `cores / heavy slots` (at least 1), counting the heavy slots of **every** queue on the machine:
heavyq's `heavy_slots` and each foreign queue's `SLOTS` from its machine file. When every heavy run starts a
test runner with one worker per core, N concurrent runs put N times the cores in runnable threads; capping each
at `cores / N` keeps a full machine near `load = cores`. Pass it to the runner (`--maxWorkers`, `-p`, `-n`,
`--jobs`, ...).

## Configuration

Machines heavyq only watches are `[[remote]]` tables with `observe_only = true`: they take `name` and `ssh`
(or `transport`) only, are never offered to the scheduler, `--remote`, `auto` or `[proof]`, and are still
probed. A key that only means something for runs (`heavy_slots`, `git_url`, `job_mb`, ...) is refused there.

```toml
[[remote]]
name = "box3"
ssh = "box3"
observe_only = true
```

Another queue is a `[[foreign]]` table. Everything is validated when the config loads; an unknown column, an
unsafe slot directory or an alias to a machine that is not configured is an error, not a guess.

```toml
[[foreign]]
name = "bashq"                            # how reports call it; not "heavyq"
history = "/var/tmp/bashq/history.tsv"    # one line per finished run (absolute or ~/)
format = "tsv"                            # the one format so far (default)
columns = ["end_epoch", "machine", "lane", "prio", "light", "wait_s", "run_s", "exit"]  # the default layout
tickets = "/var/tmp/bashq"                # live tickets: <prio>-<20-digit unix ns>-<pid>-<lane>, ".run" while running
infra_exits = [97, 124]                   # this queue's own infrastructure exit codes
machines_dir = "/srv/ops/machines.d"      # <machine>.env files: SLOTS="s1 s2", LIGHT_SLOTS="s3", KIND=...
slot_dirs = { box3 = ".bashq/slots" }     # per machine: the directory of its slot lock files there
aliases = { "build-1" = "local" }         # a machine name the queue writes -> the heavyq machine name
```

- **history**: tab-separated, one run per line, in the order runs ended. `columns` names them; a permutation or
  a subset works, but `end_epoch`, `machine` and `exit` are required. It is read **backwards from its end**,
  only as far as the window, and never more than 32 MB: a history that grows for years costs what the window
  holds. A partial window says so in the report.
- **tickets**: a directory of files named as above. The first line of a ticket may carry `KEY=VALUE` words;
  `machine=`, `slot=`, `claimed=` (unix seconds) and `cmd=` (the rest of the line) are read, nothing else.
- **machines_dir**: the files are parsed, never sourced. Only `SLOTS`, `LIGHT_SLOTS` and `KIND` are read;
  `*.env.bak` and other names are ignored.
- **slot_dirs**: probed on that machine (absolute, `~/`, or relative to its home). Every `*.lock` file there is
  a slot, busy while it is `flock(2)`-held. Give a directory that holds only that queue's slot files.

At least one of `history` and `tickets` is required.

## JSON

`heavyq fleet --json` prints one object, [`schema/fleet.schema.json`](../schema/fleet.schema.json) (schema 1;
fields may be added within it):

```json
{
  "schema": 1, "generated_at": "...", "since": "...", "window_s": 86400,
  "machines": [{
    "name": "box3", "role": "observe_only", "reachable": true, "cores": 18, "load1": 45.1, "load_per_core": 2.51,
    "free_mb": 900, "total_mb": 32000, "pressure": {"cpu_some_avg10": 61.5, "mem_some_avg10": 3, "mem_full_avg10": 1.2},
    "overload": true, "overload_why": ["load/cores 2.51 >= 1.5", "free RAM 900 MB < 10% of 32000 MB"],
    "slots": [{"owner": "bashq", "busy": 4, "total": 6}],
    "top": [{"owner": "bashq", "lane": "api", "comm": "node", "procs": 18, "threads": 220, "cpu_pct": 1650, "rss_mb": 9200, "cmd": "node jest ..."}],
    "outside_rss_mb": 4100,
    "worker_cap": {"workers": 4, "cores": 18, "heavy_slots": 4, "why": "18 cores / 4 heavy slots (bashq 4) = 4 workers per run"},
    "window": {"runs": 60, "run_hours": 15, "wait_p50_s": 1800, "wait_p90_s": 3240, "wait_p95_s": 3420, "run_p50_s": 900,
               "per_hour": 2.5, "red": 9, "red_infra": 3, "red_code": 6, "infra_rate": 0.05, "code_rate": 0.1}
  }],
  "queues": [{
    "name": "bashq", "kind": "foreign", "window": {"runs": 60, "...": "..."},
    "machines": [{"machine": "box3", "window": {"...": "..."}}],
    "running": [{"lane": "api", "prio": 1, "machine": "box3", "slot": "s1", "wait_s": 120, "run_s": 600, "cmd": "..."}],
    "waiting": [{"lane": "web", "prio": 1, "wait_s": 2580, "cmd": "..."}],
    "stale": 0, "history_truncated": false
  }],
  "notes": []
}
```

A machine that did not answer has `"reachable": false` and an `"error"`. A machine that answered with its
numbers but not with everything in time has `"reachable": true` and `"partial"`, for example
`"no top consumers and attribution (timed out after 30s)"`; its `top` is then empty. `notes` lists what could
not be read (a missing tickets dir, a partial history).

## Every 10 minutes, without a daemon

heavyq has no background process; run the report from cron or a systemd user timer and let agents read the
markdown file. The write is atomic (a temporary file in the same directory, then a rename), so a reader sees
the previous report or the new one, never half of one.

cron:

```
*/10 * * * * heavyq fleet --md "$HOME/.local/state/fleet.md" >/dev/null 2>&1
```

systemd user timer, `~/.config/systemd/user/heavyq-fleet.service`:

```ini
[Unit]
Description=heavyq fleet report

[Service]
Type=oneshot
ExecStart=%h/.local/bin/heavyq fleet --md %h/.local/state/fleet.md
```

and `~/.config/systemd/user/heavyq-fleet.timer`:

```ini
[Unit]
Description=heavyq fleet report every 10 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=10min

[Install]
WantedBy=timers.target
```

```bash
systemctl --user daemon-reload && systemctl --user enable --now heavyq-fleet.timer
```

A report takes about one second plus the slowest machine (each probe samples CPU for one second), and at most
`--timeout` per machine; machines are probed at the same time. The command never runs on the hook path.
