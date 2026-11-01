# Leases: one agent per item

Parallel agents, each in its own worktree of one repository, must never work on the same item. A **lease** is
the claim an agent holds on an item while it works on it.

> Status: this page describes the lease store (`internal/lease`). The `heavyq item claim|release` commands
> and the hooks that renew leases are not released yet; today only the Go package exists.

## Where leases live

Under the git **common** dir, so every worktree of the repository sees the same state, and nothing is ever
committed:

```
<git-common-dir>/heavyq/leases/<item>.json   one lease per claimed item (mode 0600)
<git-common-dir>/heavyq/leases/.lock         flock taken for every claim, renew, release and reclaim
<git-common-dir>/heavyq/events.jsonl         append-only audit log (mode 0600)
```

The directories are created with mode 0700. Item ids may contain `/` (ids derived from plan files, such as
`plans/foo/phase-01`); the file name replaces `/` with `+`, which ids cannot contain, so names are unique and
can never escape the directory.

## Item ids

`[a-z0-9][a-z0-9._/-]{0,127}`, the anchor rule plus `/`. Stricter on top: no empty, `.` or `..` path
segments and no trailing slash. Anything else is refused, never normalised.

## A lease

| Field | Meaning |
|---|---|
| `item`, `lane`, `session` | what is held, and by which agent session |
| `acquired`, `renewed`, `expires` | times; the default TTL is 30 minutes, every renewal grants a full TTL again |
| `pid`, `start` | **optional** owner process and its start token (`internal/procinfo`); `pid` is 0 when none was given |

## Liveness and owner identity

**Liveness is the TTL**: a lease is stale when `now > expires`. Claimers are short-lived CLI calls and hooks
(`heavyq item claim`), so their pid dies right after the claim and cannot be the signal. Leases are kept
alive by renewals: the PostToolUse heartbeat (`Renew`), and any claim by the same session.

**Pid evidence is optional.** `NewOwner(lane, session, 0)`, the CLI default, records no pid and does no pid
check. A caller with a long-lived process that vouches for the session (a wrapper such as
`heavyq exec codex`) passes its pid (`NewOwner(lane, session, pid)`, or `CurrentProcess(lane, session)` for
the calling process). The lease then also turns stale early when that process is dead, or when its pid was
reused (the start token no longer matches), so a crashed wrapper frees its items without waiting for the TTL.

**Same owner** = same lane and session; pid is never part of identity. A repeated claim or renewal by the
same session extends its lease. It refreshes pid and start only when the caller supplies a pid; a caller
without one keeps the evidence already stored. Two agents that share lane and session are one owner and do
not exclude each other.

## Operations

| Call | Behaviour |
|---|---|
| `Claim(item, owner)` | free: claim. Same owner: renew. Held by another live, unexpired owner: `*HeldError` (`errors.Is(err, ErrHeld)`, carries the holder). Stale: reclaim |
| `ClaimNext(candidates, owner)` | first claimable candidate in order, under one lock, so concurrent callers get different items. `ok=false` when none. An item the owner already holds counts as claimable (renewed) |
| `Renew(item, owner)` | holder only; works on the holder's own expired lease until someone reclaims it |
| `Release(item, owner, force)` | holder only; `force` releases anyone's lease (the event is marked `forced`) |
| `List()` | every lease, sorted, with `stale` set when expired or the explicit owner pid is gone. Lock-free |

A lease is **stale** when it has expired, or when its explicit owner pid is dead. Claims reclaim stale leases and log a
`reclaim` event naming the previous holder and the reason (`expired`, `dead` for a dead explicit pid; `corrupt` for an unreadable
file, which is overwritten).

## Events

One JSON object per line, written with a single `O_APPEND` write:

```json
{"at":"2026-11-02T00:36:50.1Z","op":"claim","item":"task-1","lane":"api","session":"s1"}
```

`op` is `claim`, `renew`, `release` or `reclaim`. Reclaims add `reason`, `prev_lane`, `prev_session`;
forced releases add `forced`. If only the log append fails, the operation still happened: the call returns
the lease together with an error wrapping `ErrEventLog`.

## Concurrency

All multi-step operations hold the `.lock` flock (`internal/slots`), polled for up to 10 s, then
`ErrLockTimeout`: a stopped process cannot hang its peers. Lease files are replaced atomically, so readers
never see a partial file. Tests race 16 goroutines and 8 real processes over fewer items than workers and
assert no item is ever handed out twice, and check that a claim from a process that exits at once stays
valid until its TTL.
