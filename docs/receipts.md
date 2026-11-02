# Receipts: a summary you can share

```bash
heavyq receipts                    # the last 7 days, exact counts, for you
heavyq receipts --day|--month      # the last 24 hours / 30 days (one window at a time)
heavyq receipts --share            # the same, rounded, with no names: safe to post
heavyq receipts --share --json     # {"version", "window", "lines", "text"}
heavyq receipts --json             # exact counts as JSON
```

It reads the ledger ([history.md](history.md)) through the bounded tail reader (`history.ReadTail` with a
`since` time), never a full scan, and writes nothing. The dashboard shows the same card in a "Receipts" panel
(week or month, "Copy to share"), fed by `GET /api/v1/receipts?window=day|week|month` (token required like
every API call; the response holds exact counts and the rounded card).

A sample card (`--share`):

```
┌─ heavyq receipts · last 7 days ─────┐
│ ~35 runs/day through the queue      │
│ ~60 duplicate runs avoided          │
│ about 1 in 4 runs waited their turn │
│ wait p50 under 1s · p95 ~7m         │
│ flagged: ~20 flaky, 5 under load    │
│ ~20 runs offloaded to 1 machine     │
│ ~40 pool values handed out          │
│ 5 agents served                     │
└─ heavyq 0.5.0 ──────────────────────┘
```

## What each number means

The window is half open: records that ended in `[now - window, now)`. A record is one finished run
(`history.md`). Records of `--light` runs, runs that gave up or were cancelled while waiting, and runs
stopped by a signal before they started are not counted anywhere (except as noted).

| Metric | Definition | Proven by |
|---|---|---|
| heavy runs | commands that ran through the queue: not `--light`, not replayed from the cache | `light`, `cache`, `result` |
| duplicate runs avoided | records with `cache` `hit` (stored result replayed) plus `coalesced` (waited for an identical run in flight) | `cache` |
| runs held back | heavy runs with `wait_s` of 1 s or more. The queue polls, so a shorter wait is jitter, not a wait. The cause (a slot, RAM or load) is not recorded, so none is claimed | `wait_s` |
| flaky | distinct `run_id`s with, in the window, a real pass and a real failure that the machine's load does not explain (see "under load"). Replays and runs without a `run_id` do not count | `run_id`, `exit` |
| under load | failed runs (`exit` != 0, ran to the end) that started over a limit of the resource gate: forced by the anti-starvation bypass, load at or above `max_load`, or less free RAM than `job_mb`. The same test as `Record.Loaded` and `gate status` | `forced`, `load1`, `max_load`, `free_mb`, `job_mb`, `cores` |
| remote runs | heavy runs whose `machine` is not `local`, and how many such machines | `machine` |
| pool values | values held by heavy runs, counted from `pools` (`port=4100,4101 pg=pg2` is 3) | `pools` |
| wait p50 / p95 | nearest-rank percentiles of `wait_s` over the heavy runs | `wait_s` |
| agents (lanes) served | distinct lanes with at least one heavy run or cache reuse | `lane` |

### Left out on purpose

- **Freezes avoided.** The machine reading (`load1`, `free_mb`) is taken when the gate admits the run, which
  is after its wait. Unless the run was forced past a limit, that reading is below the limits by
  construction, so the ledger cannot show "held back while the machine was over its limits". A forced run
  shows the opposite (it was let in over a limit). Rather than guess, the metric is not reported.
- **Runs that would have collided / overlapped.** Only "held back" is proven: a record does not say which
  other runs were active. The card says "waited their turn", not "would have overlapped".
- **Premature "done" caught.** There is no stop gate record in the ledger yet.

## What `--share` guarantees

- Counts are rounded: exact below 10, then to 5, 50, 500, ... by size (`~40`), and per-day rates the same way
  (`~40 runs/day`; a window under one run a day says "N runs in total"). Shares become words: "about 1 in 8
  runs", "about half of runs", "most runs", "nearly all runs", "fewer than 1 in 100 runs". Waits become
  coarse (`~3s`, `~2m`, `~1.5h`).
- Zero metrics are left out of the card.
- No lane, repository, worktree, command, path, session, item, gate, commit, tree or machine name is read
  into it: the aggregation only keeps counts, and a test feeds a ledger full of such names and checks that none
  reaches the card, its JSON or the exact summary.
- It carries the heavyq version and the window, and is at most 12 lines of box-drawing text.
