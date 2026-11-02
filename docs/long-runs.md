# Long runs that report themselves

This is the 10-minute path for one engineer running Claude Code agents on a long run: a harness turn, a
multi-step workflow, or an unattended night. Each step names the command and links to the reference that
owns the details. Every command below exists in heavyq v0.5 (`heavyq <command> --help`).

```
preflight ──► agents claim items ──► item done --run (proof on a dev box) ──► statusline, top, flow
    ▲                                                                              │
    └──────────── fix the NO-GO items, re-run ──────────────── recap --run <id> ◄──┘ (afterwards)
```

Before you start: heavyq v0.5 on `PATH` (`heavyq version`), a git repository, and Claude Code. Install:
[install.md](install.md).

## 1. Install the hooks

```bash
heavyq hook install claude --scope user --features queue,brief,stop,stance,flow
```

| Feature | Hook | What it does |
|---|---|---|
| `queue` | `PreToolUse` (Bash) | runs heavy commands through the queue, with a priority |
| `brief` | `SessionStart` | injects the brief (goal, the lane's items, the next command) at start, resume and after `/compact` |
| `stop` | `Stop`, `SubagentStop`, `TaskCompleted`, `TeammateIdle` | the stop gate; it stays off until you run `heavyq steer guard` (step 8) |
| `stance` | `UserPromptSubmit` | injects a stance when one of its keywords appears |
| `flow` | `PermissionRequest`, `Notification`, `StopFailure`, ... | observes where an agent waits; it never decides a permission |

`--scope user` writes `~/.claude/settings.json`. The installer keeps your other keys, writes a `.bak` first,
and is idempotent. `--features` is the exact set: installing a smaller set removes the others. Foreign hooks
are never touched. Reference: [claude-code.md](claude-code.md).

## 2. Statusline

Claude Code has one status line. Wrap yours so heavyq's segment is appended, and keep your command as it is:

```json
{
  "statusLine": {
    "type": "command",
    "command": "heavyq statusline --wrap 'node \"$HOME/.claude/statusline.cjs\"'",
    "refreshInterval": 30
  }
}
```

Replace the quoted command with your own status-line command. `refreshInterval` re-runs the line every N seconds
(optional, minimum 1). It keeps the clock and the staleness check moving while the session is idle. The
segment reads two small files and never runs git, so the timer costs almost nothing.

| Shown | Meaning |
|---|---|
| `◆ 5/8 proven` | proven items out of all items. A ticked box is not proof |
| `⚑ 1 needs you` (red) | something only a person can supply: a blocked item, a permission prompt, a credential |
| `▲ CI wait` | the constraint, shown only when it is significant |
| `✓ on track · ~16:40` | the forecast: the ETA, when the remaining items should be proven |
| `⚠ at risk · ~17:15` | the ETA is past the run's expected end, or nothing has been proven for twice the median |
| `✓ done` | every item is proven |
| (nothing) | nothing to say, or no flow state yet |

`--ascii` (or a non-UTF-8 locale) swaps the glyphs for `# ! ^ + .`. Full rules: [statusline.md](statusline.md).

## 3. Anchor: goal and items

The anchor is a checklist in git, `.heavyq/anchor.md`. Each item carries a proof:

```markdown
# Goal: ship login v2

- [ ] login-api: POST /login returns a token  `proof: gate:login-api`
- [ ] ui: login form  `proof: gate:web`  `needs: design sign-off`
- [ ] docs: document the endpoints  `proof: gate:docs`
```

Each `proof: gate:<name>` names a gate in `heavyq.toml`, which lists the commands that must all pass:

```toml
[[gate]]
name = "login-api"
# `go test -run` with no matching test still exits 0, so grep for the test's own PASS line
run  = ["go test -count=1 -run '^TestPostLogin$' -v ./auth/ | grep -q -- '--- PASS: TestPostLogin'"]

[[gate]]
name = "docs"
run  = ["python3 -I .github/scripts/check-md-links.py"]
```

Use `gate:` proofs, not `proof: <shell command>`. A shell-string proof can only run as
`heavyq ... -- sh -c '...'`, and a harness that vets commands (for example a subagent's worktree guard) refuses
that. A gate needs no typed `sh -c`, and its commands sit in `heavyq.toml`, where you can read them before the run.
`heavyq preflight` warns about a shell proof and suggests the gate form.

Item rules that matter tonight:

- an item with no proof can only be ticked by hand, so it is never proven. Preflight blocks on it;
- an item needing a human gets `needs: <what>`; `item block` records the same thing from the command line;
- ids are `a-z 0-9 . _ -`, and the first `proof:` on a line wins.

Reference: [anchor.md](anchor.md), [gates.md](gates.md), [items.md](items.md).

## 4. Preflight: front-load the human

```bash
heavyq preflight --hours 8 --parallel 3
```

```
preflight: NO-GO (2 to handle now, 1 to watch)
1. [anchor] item docs has no proof: it can only be ticked by hand, never proven -> edit .heavyq/anchor.md:5: add `proof: <command>` to docs
2. [permissions] `go test ./auth/...` may prompt -> edit .claude/settings.local.json: add "Bash(heavyq 4 --lane api --max-wait 300s -- go test *)" to permissions.allow
3. (watch) [environment] 3 lanes planned but 2 heavy slots -> heavyq preflight --parallel 2
```

Preflight checks the anchor, dependencies, disk, remotes, slots, Claude Code permissions and quota. Each
finding ends with the command or edit that fixes it. Answer the numbered blocks, then run it again.

- **GO** (exit 0) opens a run span. `--start` opens one on NO-GO too.
- **NO-GO** (exit 1) lists what a human must do now. Nothing unattended should start before GO.
- `--hours` is the planned length, so quota is judged for the run and the span gets an expected end.

For headless runs (`claude -p`), add `--permission-prompts none`: a missing rule becomes a readable denial,
not a hang. Reference: [preflight.md](preflight.md).

## 5. The agent loop

Each agent works one item at a time:

```bash
heavyq item ls                        # open, claimed, blocked, proven
heavyq item claim --next              # lease the first open item that fits one lease (30 min, renewed by runs)
# ... the agent does the work and commits it ...
heavyq item done login-api --run      # run its proof through the queue; tick the box only on a pass
```

Put the proof on a dev box so the laptop stays light. In `~/.config/heavyq/config.toml`:

```toml
[proof]
remote = "linuxi"      # "local" (default) | "auto" | the name of a [[remote]]
```

A repository's `heavyq.toml` can set its own `[proof] remote`, and `--remote` on the command wins over both.
A remote runs the **committed** code. With uncommitted edits, `auto` falls back to this machine and a named
remote is refused, so commit first or pass `--remote local`. Reference: [items.md](items.md#where-the-proof-runs),
[remote.md](remote.md).

**The size gate.** `item claim` refuses an item the lane cannot finish and prove inside one lease: a missing
proof, or a proof whose ledger p50 times 3 iterations is over 30 minutes. The refusal exits 1 with a `next:` line:

```
heavyq: item api is too big for one 30m lease: its proof takes about 11m40s (p50), times 3 iterations is 35m, over the 30m budget
next: split api into ≤3 items, each with a faster proof
```

Split the item. `--force` claims anyway; avoid it. A proof that never passed here has no known duration, so it
is claimed with a warning.

**Parallel worktrees.** `item done` ticks a box in the anchor, and two agents ticking adjacent lines conflict in
git (#119). Until that is fixed, restore `.heavyq/anchor.md` before committing, and let the lead tick the boxes on
`main`. The proof still counts: it is judged on the tree, not on the box.

## 6. While it runs

- **Statusline** (step 2): the glyph line, refreshed on each update and on the timer.
- **Notifications**: a local push when something needs you, the forecast turns at risk, or everything is proven.
  Each state change pushes once, at most 5 a day. `HEAVYQ_NOTIFY=off|terminal|desktop` picks the channel,
  `HEAVYQ_NOTIFY_BUDGET=0` turns pushing off, and `--no-notify` silences one call.
- **`heavyq top`**: a monitor for a pane beside the agents. `NEEDS YOU` comes first. `--mode focus` shows only that.
  `heavyq top --line` prints one line for tmux `status-right`. `--control` adds cancel, priority and pause.
- **`heavyq flow show`**: the state with needs-you first, then the constraint and throughput. `--json` gives the
  document. A usage limit appears as `needs you: heavyq stop_failure: rate_limit`. It clears when the lane runs
  again (see #120 in step 9). Wait for the window to reset, or resume the lane.
- **`heavyq brief`**: exactly what an agent gets after compaction. Run it when an agent seems to have lost the thread.
- **`heavyq gate status`**: is the main gate proven on the tree checked out now? Exit 0 yes, 1 no.

Reference: [top.md](top.md), [flow.md](flow.md), [statusline.md](statusline.md), [steer.md](steer.md).

## 7. Afterwards: recap

```bash
heavyq run current              # the open run span, with its id
heavyq recap --run <id>         # progress, forecast, the one best next action, and the buckets
heavyq run end                  # close the span
```

Recap buckets, each line ending with the command that acts on it:

| Bucket | Meaning |
|---|---|
| merged into main | proven, and the proven commit is reachable from the default branch |
| proven, not on main | proven, not reachable from main (a squash merge reads this way: recap does not claim more than git shows) |
| claimed, unproven | ticked with nothing proving it now: `item done <id> --run` |
| blocked | waiting on a human: `item claim <id>` once answered, `heavyq top` for a prompt |
| wasted | the same failing run repeated, with the time it cost: `heavyq why <lane>` |
| in flight | held by a lane until the lease runs out |

`heavyq recap --json` is the same document for an agent. Reference: [recap.md](recap.md).

## 8. Steering: the stop gate

The stop gate is off until you choose a mode for the repository:

```bash
heavyq steer guard      # Stop blocks while the lane holds claimed items that are not proven on this tree
heavyq steer destroy    # no cap, and SubagentStop, TaskCompleted and TeammateIdle are gated too
heavyq steer off        # the default; HEAVYQ_STEER=off is the kill switch for one shell
```

- `guard` blocks at most 3 times per session, and each block names the exact command
  (`heavyq item done <id> --run`) and the two ways out: `heavyq item block <id> "needs: ..."` or
  `heavyq item release <id>`.
- `destroy` has no heavyq cap. Claude Code itself ends a turn after 8 Stop-hook blocks in a row, so lift that
  with `heavyq steer destroy --apply [--scope local|project|user]`. It writes
  `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=0` into the settings `env` block, keeps a `.bak`, and is idempotent.

Pre-existing red is never blamed on the agent: a proof that already failed when the session started does not block.
Reference: [steer.md](steer.md).

## 9. Known limits in v0.5

Checked against the open issues at release time. Items fixed before the release are removed from this table.

| Issue | Limit | Workaround |
|---|---|---|
| [#119](https://github.com/LysCongThanh/heavyq/issues/119) | `item done` ticks the anchor in the agent's worktree; parallel ticks conflict | restore `.heavyq/anchor.md` before committing; the lead ticks on `main` |
| [#120](https://github.com/LysCongThanh/heavyq/issues/120) | a rate-limit `needs you` can outlive the pause; lanes of removed worktrees linger in `flow show` | ignore a stale line after the window resets; it drops after 24 h |
| [#127](https://github.com/LysCongThanh/heavyq/issues/127) | agents in `.claude/worktrees/` make the main checkout's tree unprovable | add `.claude/worktrees/` to `.git/info/exclude` |
| [#128](https://github.com/LysCongThanh/heavyq/issues/128) | an unknown key in the **user** config makes an older binary refuse every command | upgrade the binary before adding a new user-config key; `heavyq doctor` lists unknown keys |
| [#114](https://github.com/LysCongThanh/heavyq/issues/114) | `internal/cli` under `-race` takes about 10 minutes, so its gate can trip the size gate | keep the gate on a faster proof, or split the item |

## See also

- [items.md](items.md): claim, prove and finish
- [gates.md](gates.md): `heavyq.toml`, what "proven" means
- [preflight.md](preflight.md), [statusline.md](statusline.md), [flow.md](flow.md), [recap.md](recap.md)
- [top.md](top.md), [steer.md](steer.md), [remote.md](remote.md), [claude-code.md](claude-code.md)
- [configuration.md](configuration.md): every key and environment variable
