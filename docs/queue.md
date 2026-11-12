# The queue

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
  `force_after` skips the load check, so nothing starves behind load it did not cause; it still waits for
  RAM (a job without the RAM for it can take down the jobs already running there). Only a ticket with one
  candidate machine gets this: one that may run on several waits for whichever has room.
- **`heavyq status`.** Shows running and waiting tickets in order, per-machine resources, who holds each
  slot, why the head ticket waits, and *who holds the load*: CPU% per lane. It gets this by walking each
  process's ancestry in `/proc` to a ticket, and lists everything else as "outside the queue".
- **Spare machines over SSH, by exact commit.** heavyq can run a job on another box. It pushes the exact
  commit (`--ref`, default `HEAD`) to a bare repo there, creates a temporary worktree, runs the command,
  streams the logs back, returns the exit code and cleans up. With no `--remote` flag, a ticket is placed
  by score (`free RAM - k x load`) only when the repo is clean and the command references nothing
  outside the repo. Otherwise it stays local.
- **Agents don't need to know.** A Claude Code `PreToolUse` hook rewrites `npm test`, `go test ./...` and
  friends into a queued heavyq run before they execute. `heavyq hook install claude` sets it up.
- **Agent mode: few tokens, a clear verdict.** When the caller is a coding agent (`CLAUDECODE=1` and friends),
  heavyq prints at most a queued line, a running line and a digest: `PASS`/`PASS?`/`FAIL`/`REUSED`, up to 12
  failure lines, the path of the full log, and one `next:` line saying what to do. See
  [Agent mode](agent-mode.md).
- **The same gate runs once per commit.** With `--cache`, identical commands on the same clean tree share
  one run: whoever comes second waits for the first and gets its output and exit code. Finished results are
  replayed until they expire.
- **History is a ledger of evidence.** Every run is appended to a JSONL log: wait time, run time, machine,
  exit code, cache hit/miss/coalesced, and *which code it ran on* (commit, tree, dirty), plus the session,
  item and gate that asked for it. "`go test ./...` passed on the code that is checked out now" becomes a
  fact you can look up. `heavyq history` prints it.

Single static Go binary. No daemon. The queue is a directory.

## Quickstart

```bash
# Wrap heavy commands. Lower priority number = earlier turn.
heavyq 3 --lane api -- npm test
heavyq 2 --lane checkout -- pnpm build            # jumps ahead of priority 3+
heavyq 6 --light --lane docs -- npm run lint      # may take a light slot right away
heavyq 4 --max-wait 10m -- cargo test             # exit 75 if not started within 10 minutes

heavyq 4 --cache -- npm test                      # same clean commit already tested? reuse that result
heavyq 4 --session "$ID" --item T-12 -- npm test  # tag the run in history: who ran it, for which work item
heavyq 4 --needs port:2,pg -- npm run e2e         # hold 2 ports and a database while it runs (docs/pools.md)
heavyq 4 --with shared-schema -- make contract    # sibling repo ../shared-schema, pinned to its HEAD, shipped with a remote run (docs/remote.md)
heavyq 4 --need-gh -- ./scripts/open-release-pr   # uses this machine's GitHub CLI login: runs here only (docs/remote.md)
heavyq 4 --digest -- npm test                     # end with the result digest an agent gets (always on in agent mode)

heavyq status      # what runs, what waits, and why (opens with a short story in plain words)
heavyq why         # just that story: "2 agents wait because every heavy slot on local is taken."
heavyq why api     # one lane: its running and waiting jobs, the reason per machine, its last run
heavyq retarget 4242 --remote spare   # a waiting job moves to other machines, keeps its place (docs/remote.md)
heavyq history     # recent runs, with the commit each one ran on (-v: tree, session, item, gate)
heavyq gate status # is this exact code proven? (heavyq.toml gates, or a detected default; exit 0 yes, 1 no)
heavyq gate run    # run what is not proven yet, through the queue, cached
heavyq init --agents-md      # write heavyq.toml from what heavyq detects, tell agents to run the gate
heavyq hook install claude   # let Claude Code agents use the queue without being told
heavyq help agents           # the 200-token usage card for a coding agent
```

With no config file you get 2 heavy slots, 1 light slot, the resource gate on, and local runs only.
