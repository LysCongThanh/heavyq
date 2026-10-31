# heavyq

**A priority, resource-aware queue for heavy commands, built for workstations running many AI coding agents at once.**

> A lock that doesn't know priority is just a polite race.

[![ci](https://github.com/LysCongThanh/heavyq/actions/workflows/ci.yml/badge.svg)](https://github.com/LysCongThanh/heavyq/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/LysCongThanh/heavyq.svg)](https://pkg.go.dev/github.com/LysCongThanh/heavyq)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

![heavyq status: running and waiting tickets by priority and lane, slot holders, CPU by lane, and why the head ticket waits](docs/demo.gif)

## The problem

You run six coding agents in six worktrees. Each one finishes a change and runs the gate: `npm test`,
`pnpm build`, `cargo test`, a Postgres-backed integration suite. Six test suites start in the same
second on one machine. Load goes to 60 on 16 cores, the OOM killer takes the slowest one, the rest time
out, and every agent retries. Nothing finishes.

The usual fix is `flock /tmp/build.lock npm test`. That serialises the work, but the kernel hands the lock
to whoever asks next. The hotfix waits behind the docs rebuild. Light jobs like lint wait behind heavy
ones. Nobody checks whether the machine has RAM left. And the spare laptop under your desk sits idle.

## What heavyq does

```bash
heavyq 3 --lane api -- npm test
```

- **Priority tickets.** Priority 1-9 (lower runs first), FIFO within a priority. A ticket is a file in a
  queue directory. If its process dies, the ticket is pruned (pid plus start time, so a reused pid
  doesn't keep it alive). Ctrl-C, SIGTERM and SIGHUP clean up. `--max-wait` gives up with exit 75.
- **Slots.** Each machine has N heavy slots and M light slots. `--light` jobs (lint, typecheck, unit tests)
  can use light slots, and jump ahead of waiting heavy jobs into them. Heavy jobs never use light slots,
  so they lose nothing. Slots are plain `flock(2)` files, so existing `flock <file> cmd` users still
  share them.
- **Resource gate.** A job starts only if available RAM covers its budget plus a reserve, and the 1-minute
  load is below `ratio x cores`. RAM for jobs that started in the last minute is held back (a new job
  hasn't grown yet). An idle machine always takes a job. A head ticket that has waited past
  `force_after` bypasses the gate, so nothing starves.
- **`heavyq status`.** Shows running and waiting tickets in order, per-machine resources, who holds each
  slot, why the head ticket waits, and *who holds the load*: CPU% per lane. It gets this by walking each
  process's ancestry in `/proc` to a ticket, and lists everything else as "outside the queue".
- **Spare machines over SSH, by exact commit.** heavyq can run a job on another box. It pushes the exact
  commit (`--ref`, default `HEAD`) to a bare repo there, creates a temporary worktree, runs the command,
  streams the logs back, returns the exit code and cleans up. With no `--remote` flag, a ticket is placed
  by score (`free RAM - k x load`) only when the repo is clean and the command references nothing
  outside the repo. Otherwise it stays local.
- **History.** Every run is appended to a JSONL log (wait time, run time, machine, exit code).
  `heavyq history` prints it.

Single static Go binary. No daemon. The queue is a directory.

## Install

```bash
go install github.com/LysCongThanh/heavyq@latest
```

Or download a binary from [Releases](https://github.com/LysCongThanh/heavyq/releases) (Linux and macOS, amd64 and arm64).

## Quickstart

```bash
# Wrap heavy commands. Lower priority number = earlier turn.
heavyq 3 --lane api -- npm test
heavyq 2 --lane checkout -- pnpm build            # jumps ahead of priority 3+
heavyq 6 --light --lane docs -- npm run lint      # may take a light slot right away
heavyq 4 --max-wait 10m -- cargo test             # exit 75 if not started within 10 minutes

heavyq status      # what runs, what waits, and why
heavyq history     # recent runs
```

With no config file you get 2 heavy slots, 1 light slot, the resource gate on, and local runs only.

## Claude Code integration

Parallel agents are the reason this tool exists. Give them one rule and one guard.

**1. A rule in `CLAUDE.md`** (global or per project):

```markdown
## Heavy commands
Run test suites, builds and other heavy commands through heavyq, never directly:
`heavyq <prio> --lane <worktree-name> --max-wait 9m -- <command>`
Priority: 2 = blocking a merge, 4 = normal gate, 6 = background checks. Add `--light` for lint/typecheck.
Exit 75 means the queue is busy: wait a few minutes and retry, don't run the command without heavyq.
Env vars for remote runs go inside the command: `-- env FOO=1 npm test`.
```

Keep `--max-wait` below the Bash tool timeout (2 minutes by default; raise it with `BASH_DEFAULT_TIMEOUT_MS`
and `BASH_MAX_TIMEOUT_MS` in Claude Code's settings `env`). Long queues are better run with the Bash tool's
`run_in_background`.

**2. A PreToolUse hook** that blocks unwrapped heavy commands and tells the agent how to rerun them:
[`examples/claude-code/require-heavyq.sh`](examples/claude-code/require-heavyq.sh) (needs `jq`).

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "~/.claude/hooks/require-heavyq.sh" }] }
    ]
  }
}
```

Set `HEAVYQ_LANE` per worktree (for example in the shell that starts the agent) so `status` shows which
agent owns which job.

## Spare machines

Add a remote in `~/.config/heavyq/config.toml`:

```toml
[[remote]]
name = "buildbox"     # used in --remote buildbox and in status
ssh = "buildbox"      # an alias from ~/.ssh/config (key auth; ControlMaster recommended)
heavy_slots = 2
light_slots = 2
cache_hook = '''
# optional: restore node_modules from a cache keyed by the lockfile
[ -f package-lock.json ] || exit 0
key=$(sha256sum package-lock.json | cut -c1-16); c="$HEAVYQ_CACHE_DIR/nm-$key"
if [ -d "$c" ]; then cp -al "$c" node_modules; exit; fi
npm ci --no-audit --no-fund && { cp -al node_modules "$c.$$" && mv -T "$c.$$" "$c" || rm -rf "$c.$$"; } 2>/dev/null; true
'''
```

Then:

```bash
heavyq 3 --remote buildbox -- npm test        # always there
heavyq 3 --remote auto -- npm test            # best score; you vouch the command is self-contained
heavyq 3 -- npm test                          # best score only if the repo is clean and paths stay inside it
heavyq 3 --remote local -- npm test           # never leave this machine
heavyq 3 --needs-ram -- npm run e2e           # place by free RAM only
heavyq 3 --remote buildbox --base-ref origin/main -- npm run lint:changed   # recreate origin/main there
heavyq 3 --remote buildbox --with ../shared-lib@v2.1.0 -- npm test          # sibling repo pinned to a commit
```

What a remote run sees, and what it doesn't:

- **Only committed code.** The exact SHA of `--ref` is pushed with `git push` (it doesn't need to be on
  GitHub). Uncommitted edits and ignored files (`.env.test`, `dist/`) stay behind.
- **Environment variables don't travel.** `FOO=1 heavyq ... -- npm test` sets `FOO` only locally. Put them
  inside the command: `heavyq 3 -- env FOO=1 npm test`.
- **The same subdirectory.** If you run from `repo/packages/web`, the command runs in `packages/web` of the
  remote worktree.
- **Clean teardown.** The remote side holds a lifeline on stdin. If heavyq dies, even by SIGKILL, or the
  connection drops, the remote stops the command's process group and removes the worktree. Exit 255 means
  the connection was lost.

Remote requirements: Linux with `bash`, `git`, `flock`, `setsid` (util-linux), `base64`, `awk`. Nothing is
installed. Everything lives under `work_dir` (default `~/.heavyq`).

## Comparison

| | heavyq | [pueue](https://github.com/Nukesor/pueue) | [task-spooler](https://viric.name/soft/ts/) | GNU parallel / `sem` | plain `flock` |
|---|---|---|---|---|---|
| Model | wrapper, no daemon; queue is a directory | daemon + client | per-user server | job runner; `sem` = counting semaphore | kernel lock |
| Priority | 1-9, FIFO within | yes (`--priority`, since 3.2) | urgent flag (`-u`) | no | no |
| Caller blocks until done, gets exit code | yes | no (`pueue wait` / `follow`) | optional (`-c`, `-w`) | yes | yes |
| Concurrency limits | heavy + light slots per machine | per-group parallelism | slots (`-S`) | `-j` / `sem -j` | 1 |
| RAM / load gate | yes, with reservation + anti-starvation | no | no | `--memfree`, `--load` (own jobs) | no |
| Light jobs jump ahead | yes, into light slots | no | no | no | no |
| CPU attribution per lane | yes (`status`) | no | no | no | no |
| Remote execution | by exact commit, temp worktree, auto placement | no (remote *clients*) | no | `--sshlogin` (copies files, no git) | no |
| Pause / resume / edit queued tasks | no | yes | partly | no | no |
| Persistent logs of each task | no (streams to caller) + history | yes | yes | `--results` | no |

Pick **pueue** if you want a long-lived personal task manager with pause, edit and logs. It is excellent,
and heavyq owes the "queue for your shell" idea to it and to task-spooler. Pick **heavyq** when many
independent callers (agents, scripts, CI shells) each need to run something heavy, wait for their turn
in priority order without overloading the machine, and get the exit code back.

## Configuration

`--config FILE`, else `$HEAVYQ_CONFIG`, else `$XDG_CONFIG_HOME/heavyq/config.toml` (`~/.config/heavyq/config.toml`).
Every key is optional. [`examples/config.toml`](examples/config.toml) has every key with comments.

| Key | Default | Meaning |
|---|---|---|
| `queue.poll` | `"2s"` | how often a waiting ticket retries |
| `queue.force_after` | `"15m"` | head ticket waiting this long bypasses the RAM/load gate |
| `queue.resource_gate` | `true` | `false` = slots only |
| `queue.auto` | `true` | score-based placement for tickets without `--remote` |
| `queue.load_weight_mb` | `500` | `k` in `score = free_mb - k x load1` |
| `queue.hold_for` | `"1m"` | how long a fresh job's RAM budget stays reserved |
| `queue.remote_down_max` | `"10m"` | remote-only ticket exits 76 after its remotes are unreachable this long |
| `queue.probe_timeout` | `"5s"` | timeout of one remote metrics probe |
| `queue.base_ref` | `""` | default `--base-ref` |
| `local.heavy_slots` / `light_slots` | `2` / `1` | slots on this machine |
| `local.legacy_lock_files` | `[]` | use these files as heavy slots (shared with `flock <file>` users) |
| `*.max_load_ratio` | `0.75` | admit while `load1 < ratio x cores` |
| `*.job_mb` / `light_mb` / `reserve_mb` | `3072` / `1024` / `2048` | RAM budget per heavy / light job, and headroom |
| `remote.name`, `remote.ssh` | required | name and ssh alias (`ssh` may be replaced by `transport` + `git_url`) |
| `remote.transport` | `["ssh","-o","BatchMode=yes","-o","ConnectTimeout=10",ssh]` | argv prefix; the remote script is appended as one argument |
| `remote.work_dir` | `".heavyq"` | remote directory (relative to the remote home) |
| `remote.git_url` | `"<ssh>:<work_dir>/git"` | where bare repos are pushed |
| `remote.cache_hook` | `""` | shell run in the worktree before the command; gets `HEAVYQ_CACHE_DIR`, `HEAVYQ_WORKTREE` |

Environment: `HEAVYQ_LANE` (default lane), `HEAVYQ_REMOTE` (default `--remote`), `HEAVYQ_AUTO=0` (never
auto-place), `HEAVYQ_DIR` (queue dir; default `$XDG_RUNTIME_DIR/heavyq`, else `/tmp/heavyq-<uid>`),
`HEAVYQ_HISTORY` (default `$XDG_STATE_HOME/heavyq/history.jsonl`), `HEAVYQ_CONFIG`. Commands run locally
get `HEAVYQ_MACHINE`, `HEAVYQ_SLOT`, `HEAVYQ_LANE`.

## Exit codes

| Code | Meaning |
|---|---|
| *command's own* | the command ran; this is its exit code (128+n if killed by signal n) |
| 2 | usage or setup error (nothing ran) |
| 75 | `--max-wait` elapsed before the ticket's turn (EX_TEMPFAIL: retry later) |
| 76 | the ticket can only run remotely and its remotes were unreachable for `remote_down_max` |
| 77 | remote preparation failed (push, worktree, cache hook, or a slot taken outside the queue) |
| 127 / 126 | command not found / not executable |
| 255 | connection to the remote lost mid-run (the remote side stops the command) |

## Platform support

Linux is the primary target and is what CI runs. On **macOS**, slots, priorities, tickets and remote runs
work. Local RAM and load are not measured (there is no `/proc`), so the local gate falls back to slots
only and `status` says so. Per-lane CPU attribution is Linux-only. Process identity uses `ps -o lstart=`.
Remote machines must be Linux.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues labelled
[good first issue](https://github.com/LysCongThanh/heavyq/labels/good%20first%20issue) are scoped to an
afternoon.

## License

[MIT](LICENSE)
