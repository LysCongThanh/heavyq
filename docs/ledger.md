# The ledger, its indexes and `heavyq doctor`

Every run ends with one JSON line in the **ledger** (`history-YYYY-MM.jsonl` in `$XDG_STATE_HOME/heavyq`, or the
one file `$HEAVYQ_HISTORY` names). The ledger is the only truth. Everything else on this page is derived from
it, can be deleted at any time, and is rebuilt with `heavyq doctor --reindex`.

Why there is more than the ledger: on a busy fleet the ledger reaches about 100,000 lines (55 MB) a month, and
a full scan costs 0.3-0.7 s. A hook that runs on every tool call has 20 ms. So no hot path scans.

## Reading the newest records: `history.ReadTail`

`history.ReadTail(path, TailLimit{Max: n, Since: t})` seeks to the end of the newest file and reads backwards in
64 KiB chunks, into the older monthly files only while the limit is not reached. It stops after `Max` records
or at the first record that ended before `Since`. Its cost follows what it returns: the last 2,000 records of a
100,000-line ledger take a few milliseconds, against a few hundred for a scan. Lines it cannot use are skipped
exactly as `ReadLast` skips them: invalid JSON, a torn last line, a line over 1 MiB. CRLF line endings are
fine. A line without a line ending at the end of the file is read like any other.

## The indexes (`internal/ledgerindex`)

They live in `index/` next to a monthly ledger, or in `<file>.index/` next to a pinned `$HEAVYQ_HISTORY`.

| File | Holds | Used by |
|---|---|---|
| `lanes/<hash>.json` | the last 50 records of one lane (run_id, cmd, tree, exit, result, the machine readings that explain a failure, `end`, ...) | `why <lane>`, the Tower (loops, give-ups, active work trees) |
| `trees/<hash>.json` | the conclusive runs on one `(repo, tree)` (newest 32), and per `run_id` the latest exit, when, and how many real runs passed and failed | `gate status`, `gate run`, `why`, the Tower's red gates, and the loop, flaky and hook checks to come |
| `meta.json` | which ledger file the index covers and its byte size, how many records were applied | every reader, to tell fresh from stale |
| `manifest.json` | which lane and tree files exist and when each was last touched | writers only (eviction) |

File names are hashes, so no value from the ledger becomes a path. Everything is bounded: 50 records per
lane, 32 runs and 64 run ids per tree, 128 lanes and 256 trees (the least recently touched is evicted first,
and `meta.json` remembers that something was evicted).

**One write path.** `ledgerindex.Append` replaces `history.Append` where a run ends. Under a lock
(`index/lock`, `flock`, so a killed process never blocks the next) it brings the index up to the end of the
ledger, appends the record, indexes it, and writes the files atomically (temp file and rename), `meta.json`
last. Only the ledger write can fail the run's bookkeeping: the index is never allowed to.

**Fresh, stale, missing.** The index is *fresh* while `meta.json`'s size equals the ledger file's size. Any
other append (an older heavyq, a lock that timed out, a crash between the ledger write and the index write)
makes it *stale*, which readers see and answer around; the next run catches up from the recorded byte offset,
without a rescan. A missing, corrupt or future-schema index is rebuilt by the next run. A month change
carries on from the old file's size.

**Readers never scan.** `TreeRuns`, `LastOfLane` and `Recent` read one to a few small files when the index is
fresh. When it is not, when a tree has more runs than the index keeps, or when a file is missing while the
index has evicted others (so it may only be evicted), they read the newest 2,000 records of the ledger
(`ledgerindex.TailWindow`) and filter. A proof older than that window is reported `missing`, never guessed.
The answers are the same as a full scan's: the tests build random ledgers and compare the proof computed from
the index with the proof computed from a scan, and the index built record by record with one rebuilt from the
ledger, byte for byte.

## `heavyq doctor [--reindex]`

```
$ heavyq doctor
ledger index: fresh
  dir:      /home/me/.local/state/heavyq/index
  lanes:    12
  trees:    256 (evicted: true)
  records:  100000
  size:     622.0K
$ heavyq doctor --reindex
reindexed 100000 records in 1.1s
ledger index: fresh
...
```

Exit 0 when the index is fresh (or there is no ledger yet), 1 when it is stale or missing (the output says
how to repair it), 2 on a usage error. `--reindex` rebuilds from the whole ledger under the writers' lock and
takes a second per 100,000 records. Deleting the `index/` directory is always safe. With remotes configured,
`doctor` also lists which common tools (and the tools `heavyq.toml` gates declare) resolve on each remote; that
part never changes the exit code (see [remote](remote.md#placed-where-its-tools-are---tools)).

## Hook deadline (`internal/deadline`)

Hooks run on every tool call and must never block an agent. `deadline.RunWithin(d, fn)` runs a handler and
waits at most `d` (`deadline.HookBudget`, 15 ms, by default): on time it returns the handler's own result;
otherwise, or if the handler panics, it returns the zero value and an error (`deadline.ErrExceeded`,
`deadline.ErrPanic`) and the hook prints nothing and exits 0. A test holds the hook path (proof of the lane's
current tree, the lane's last run and the recent window, through the index) under 20 ms on a 100,000-record
ledger, and CI runs it without the race detector.

## Measured

On a 100,000-record synthetic ledger (about 55 MB), on a 16-thread laptop that was busy (load average about
9), best of several runs:

| What | Time |
|---|---|
| full scan with `ReadLast` | 220-270 ms |
| `ReadTail`, last 2,000 records | 5.4 ms (median 7.5 ms) |
| hook path through the index (proof, last run of the lane, recent window) | 4.6 ms (median 5.4 ms) |
| the same path with the index missing (tail fallback) | 8.5 ms (median 9.9 ms) |
| `doctor --reindex` | about 1.1 s |

Most of a tail read is decoding: a hand-written decoder for the flat ledger lines (about 1.8 us a line, 4x
`encoding/json`) handles what heavyq writes and hands anything unusual to `encoding/json`, so results and errors
are identical (a fuzz target compares them).

Run them with `go test ./internal/history ./internal/ledgerindex -run 'Latency|HookPath' -v`.
