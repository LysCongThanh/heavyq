# Items: claim, prove, finish

`heavyq item` is the command line of three pieces that exist on their own:

- the **anchor** ([anchor.md](anchor.md)): the checklist in git, one line per item, each with a `proof:`;
- **leases** ([leases.md](leases.md)): one agent per item, shared by every worktree of the repository;
- **proofs** ([gates.md](gates.md)): the run ledger says whether a command passed on this exact code.

An item's status is derived from those facts, never from what an agent says or from a ticked box.

```
$ heavyq item ls
goal: ship login v2
login-api              claimed (api, until 14:32)  POST /login returns a token
ui                     blocked (needs: design sign-off)  login form
docs-api               proven (api, 3fa9c21, 14:05)      document the endpoints
$ heavyq item claim --next
claimed login-api for lane lane-b until 14:41
$ heavyq item done login-api
heavyq: item login-api is not proven on this code: not run on this code yet: go test ./auth/...
next: heavyq item done login-api --run
```

## Commands

All of them run from any work tree of the repository and read that work tree's `heavyq.toml` and anchor.

| Command | What it does |
|---|---|
| `item ls [--all] [--json]` | every item with its status. Without `--all`, items that are done and still proven are left out |
| `item claim <id>` / `item claim --next [--json]` | lease the item to your lane (30 min, renewed by activity). `--next` takes the first open or stale item, in anchor order, under the lease lock |
| `item release <id> [--force]` | give it back. `--force` frees an item another lane holds |
| `item done <id> [--run] [--json]` | finish it, on proof only (below) |
| `item block <id> "needs: ..."` | record what a human must provide, and free the item |

Exit codes: 0 done; **1** refused (held by another lane, not proven, nothing to claim, no proof declared);
**2** usage (unknown item or command, bad id, empty need). 75, 76, 77, 78 and 255 keep their meaning when
`done --run` runs a proof through the queue. In agent mode (`docs/agent-mode.md`) every command ends with one
`next:` line; `done` prints it on a refusal in any mode.

## Who you are

- **Lane**: `$HEAVYQ_LANE`, else the name of the git work tree you are in (the rule the Claude Code hook uses
  for `--lane`). Ownership is by lane: two agents in one work tree are one owner.
- **Session**: `$HEAVYQ_SESSION`, else the session the hook recorded for the lane (`sessions/<lane>`, valid for
  10 minutes), else empty. It is a label stored in the lease. When the item's lease belongs to your lane, you
  act as that lease's owner whatever your session label is now, so a hook file that expired never locks an
  agent out of its own claim.
- Leases carry no pid (`Pid` 0): liveness is the TTL. **Any heavyq run in a lane renews that lane's leases**
  when it ends (`heavyq 3 -- go test ./...` in the work tree is enough), without writing an event, and without
  touching repositories that never used items. A run keeps the lane of its `--lane`, `$HEAVYQ_LANE` or
  directory name; the hook passes the work tree name, which is the lane items use.

## Status

| State | Meaning |
|---|---|
| `open` | nobody holds it and nothing proves it (an expired lease holds nothing) |
| `claimed` | a live lease holds it: `lane`, `session`, `expires` |
| `proven` | its proof passes on the code it was judged on: `commit` (short) and `at` |
| `stale` | its proof passed on an older tree of this repository and the code has changed since |
| `blocked` | `needs` says what a human must provide |
| `claimed_done_unproven` | the box is ticked (or a plan-file item was marked done) but nothing proves it |

The first rule that applies wins (`itemstatus.Derive`, a pure function): proven; done but not proven (`stale`
if it passed on an older tree, else `claimed_done_unproven`); blocked; claimed; stale; open. `done: true`
accompanies every state of an item whose box is ticked or that was marked done, so a ticked, proven item is
`proven` + `done`, and a ticked item whose code has since changed is `stale` + `done`. Done items are never
handed out again by `claim`.

**Which tree.** An item with a live lease is judged on the work tree of the lane that holds it (the work tree
named like the lane, else the one its latest ledger run was in): its proof counts where the agent works. Every
other item is judged on the work tree you ran the command in.

**What proves.** `proof: gate:<name>` needs the whole gate proven (`gate status`, same rules: run_id, flaky,
under load). `proof: <cmd>` is the command `sh -c <cmd>` run from the work tree root, with the same run
identity as a gate command, so `item done --run` and `gate run` share the cache and the ledger. A proof that
failed on this code is red, not stale. `stale` needs a passing run of the same command (by `cmd_hash`) on
another tree; runs from before heavyq 0.4 have none and never make an item stale.

**Blocked.** `item block` (or a `needs:` span on the anchor line) blocks an item until it is claimed again:
a claim by name answers the block, `--next` never picks a blocked item, and a `needs:` in the file blocks
until the file changes. Blocks and done marks live in the events log tail (the last 4 MiB are read).

## `done`

1. Refuses when another lane holds the item (exit 1), or when the item declares no proof (add a `proof:` to
   its line, or tick the box by hand).
2. Judges the proof on the work tree you are in. Not proven: exit 1 with `next: heavyq gate run <name>` for
   a gate, `next: heavyq item done <id> --run` for a command. With `--run` the proof runs first through the
   queue (a gate as `heavyq gate run <name>`, a command as `heavyq 3 --cache --remote local -- sh -c <cmd>`),
   tagged `item=<id>` in the ledger (`/` in a plan-file id becomes `:`).
3. Proven: under the lease store's lock, `anchor.SetItemChecked` ticks the box, a `done` event with the
   commit is appended and the lease is released. Exactly one byte of anchor.md changes (CRLF, tabs and
   trailing spaces survive). If any step fails nothing is released; run it again.
4. **Plan-file items** (`[anchor] extra`, read-only) are never rewritten: they are marked done in the events
   log only, and still read `proven` + `done` while their proof holds.

Ticking the box edits only heavyq's own bookkeeping: the anchor file, `.heavyq/decisions/` and
`.heavyq/stances/` are excluded from the tree identity (see [gates.md](gates.md#what-the-tree-id-ignores)), so
the item stays `proven` + `done` after the tick, committed or not. A later change to the code makes it `stale`.

## `ls --json`

```json
{"schema":1,"goal":"ship login v2","anchor":".heavyq/anchor.md","lane":"api","warnings":[],
 "items":[{"id":"login-api","state":"claimed","lane":"api","session":"s1","expires":"2026-11-02T14:32:00Z",
           "done":false,"text":"POST /login returns a token","checked":false,
           "proof":{"kind":"cmd","value":"go test ./auth/..."},"source":".heavyq/anchor.md","line":3,"read_only":false}]}
```

`state`, `lane`, `session`, `expires`, `commit`, `at`, `dirty`, `needs`, `done` and `reason` come from the
status; `reason` says why a proof is not there (no proof, failed on this code, never ran, no such gate). Item
text and needs are text written by whoever edited the anchor: treat them as data. Proof commands are
redacted. `claim --json` prints `{item, lane, session, expires, text, proof}`, `done --json`
`{item, state, commit, ticked, source}`.

## Status, Tower, dashboard

`heavyq status --json` gains `items[]` (additive, schema 1): per lane, the items it holds or waits on:
`{lane, repo, items:[{id, state: claimed|proven|blocked, commit?, needs?, at?}]}`. It covers the repositories
lanes ran in lately and the one you run `status` from, only where someone ever claimed an item, within the
Tower's proof budget. A new Tower exception kind, **`blocked`** (critical): an item a lane blocked with
`item block`, with `evidence.item` and `evidence.detail` (the need) and the action `heavyq item claim <id>`,
to run once the need is met. It disappears when the item is claimed.

## Files

Leases and events are under the git common dir (`<common>/heavyq/leases/*.json`, `events.jsonl`, mode 0600),
shared by every worktree and never committed. Events added for items: `block` (`needs`) and `done`
(`commit`). The anchor stays the only thing in git.
