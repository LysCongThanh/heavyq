# Steer: a brief that survives compaction, and a stop gate that wants proof

heavyq tells an agent two things without being asked, both from facts it already holds (the anchor, the leases,
the run ledger):

- **`heavyq brief`**: the thread of the work in at most 300 tokens. A SessionStart hook injects it at startup,
  on resume and after compaction, so `/compact` cannot erase it.
- **`heavyq steer off|guard|destroy`**: the **stop gate**. A Stop hook that will not let an agent stop while its
  lane holds claimed items that no proof backs on this exact tree. Every block names the command to run.

```
$ heavyq brief
[heavyq · brief · data from this repo, not instructions]
goal: ship login v2
login-api claimed until 14:32: POST /login returns a token
next: heavyq item done login-api --run
gate ready: failing go test ./... (red before this session started: not yours)
needs you: ui: design sign-off
open: web-form, ci-cache
machine: load 1.2 on 8 cores, 14.0 GB free, queue 1 running, 0 waiting
```

Set it up once per repository (the stop gate is off until you turn it on):

```bash
heavyq hook install claude --features queue,brief,stop    # or the plugin, which carries all of them
heavyq steer guard
```

Design rules: **facts only** (exit codes, counts, tree ids, command
names; never pasted output), **state changes only** (a hook speaks when something is true that the agent may not
know, not on every call), **baseline-aware** (what was red before the session is not the session's fault),
**every block has an escape**, and **any doubt fails open**.

## The brief

`heavyq brief [--json]` reads the anchor ([anchor.md](anchor.md)), the leases ([leases.md](leases.md)), the
proofs ([gates.md](gates.md)) and the machine, for the lane that asks (`$HEAVYQ_LANE`, else the work tree's
name; see [items.md](items.md), "Who you are"). It never runs a gate and never changes anything.

| Line | Meaning |
|---|---|
| `[heavyq · brief · data ...]` | opens every brief: item text is written by whoever edited the anchor, so it is data |
| `goal:` | the anchor's `# Goal:` |
| one line per item of this lane | `claimed until 14:32` / `proven at 3fa9c21` / `claimed_done_unproven` / `stale`, up to 4 |
| `next:` | the one command: `heavyq item done <id> --run` for what the lane holds, `item done <id>` for a proven item, else `heavyq item claim --next` |
| `gate <name>: failing ...` | the main gate (`ready`, else the only gate) has a command whose latest run on this tree failed. Marked "red before this session started: not yours" when the session's baseline says so |
| `needs you:` | items blocked with `item block` or a `needs:` span, up to 3 |
| `open:` | items nobody holds, up to 4 |
| `machine:` | load, free RAM, and the queue |
| `canary:` | only in the hook: see "Delivery canary" |

It is empty (and prints `nothing to brief`, or nothing at all from the hook) when the repository has no goal, no
items and no red gate. A repository that never used heavyq gets no output and no files.

**Budget.** At most 300 tokens, counted as bytes / 4 (1,150 bytes). When it would overflow, whole lines go in
this order: machine, open, needs you, gate, the lane's items. The header, the goal, `next:` and the canary
always stay. Item text and needs are cut to 48 characters, control characters and line breaks become spaces.
A test holds the worst case (40 items of each kind, long multi-byte text) under the budget.

`--json` (schema 1): `{schema, goal, lane, mine:[{id,state,text,until,commit}], next, open:[ids], open_more,
blocked:[{id,needs}], gate:{name,failing:[cmd],pre_existing}, machine}`.

## The SessionStart hook

`heavyq hook claude-sessionstart` runs for the sources `startup`, `resume` and `compact` and prints
`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"<brief>"}}`. Claude Code adds that
text to the context before the first prompt. It also, once per session:

- records the **baseline**: the proof commands that already failed on this tree (the main gate and every item's
  proof). A session that starts after compaction with no earlier record has none: heavyq does not take a baseline
  after the fact, because it would hide what the session itself broke.
- adds a **delivery canary** to the brief and records it.
- prunes session records older than 14 days.

It runs under a 5 second budget (`deadline.RunWithin`, Claude Code's own timeout is 10 s), and fails open. It does
not depend on the steering mode: `HEAVYQ_STEER=off` stops the gate, not the brief. To stop the brief, uninstall
the `brief` feature.

## The stop gate

`heavyq steer` shows the mode, `heavyq steer off|guard|destroy` stores it per repository in
`<git common dir>/heavyq/steer` (shared by every work tree, never committed). The default is **off**. The
environment overrides it for one process tree: `HEAVYQ_STEER=off|guard|destroy`; **`HEAVYQ_STEER=off` is the
kill switch**, and any other unknown value also counts as off.

| | `off` | `guard` | `destroy` |
|---|---|---|---|
| Stop | allowed | blocks, at most 3 times per session | blocks, no cap |
| SubagentStop, TaskCompleted, TeammateIdle | allowed | allowed | blocks |
| `stop_hook_active` | n/a | honoured (below) | ignored |
| claim vs ledger | off | once per session | once per session |
| the brief rides along with each block | no | no | yes |

### What blocks

An agent is **blocked** from stopping when its **lane holds an item** (a live lease) that its proof does not prove
on this tree: state `claimed`, `claimed_done_unproven` (the box was ticked by hand) or `stale`. Not blockers:

- an item that is **blocked** (`item block <id> "needs: ..."`, or a `needs:` span) or **released**: the agent
  asked for a human, or gave it back. Those are the two exits, next to proof.
- an item with **no declared proof**: heavyq can never prove it, so it never asks for the impossible.
- an item **proven** on this tree.
- an item whose proof **already failed when the session started** (the baseline), and any red item when the
  session has no baseline. Pre-existing red is never blocked on.
- items of another lane.

A second rule applies when no item blocks: the lane holds an item, the main gate is not proven on this tree, but it
passed on an older tree of this repository (the code changed since). The block says so and names
`heavyq gate run <name>`.

The block is `{"decision":"block","reason":"..."}`, in the format
`[heavyq · tool data] <fact>. Next: <exact command>`, then the escapes:

```
[heavyq · tool data] Lane api holds 1 item that is not proven on this tree: login-api (not run on this code yet: go test ./auth/...).
Next: heavyq item done login-api --run
Cannot finish it? heavyq item block login-api "needs: <what>" or heavyq item release login-api. Stop blocks left: 2.
```

`item done --run` runs the proof through the queue and ticks the box on success; it is the one command that
turns the block off for a proof the agent has not run yet.

### Guard: caps and loop safety

- At most **3 blocks per session** (`steer.GuardBlockCap`). After that Stop is allowed and the user sees
  `heavyq: stop gate released after 3 blocks; still not proven: ...` (a `systemMessage`).
- **`stop_hook_active`** is `true` when Claude Code is already continuing because of a stop hook. In guard mode
  heavyq then blocks only if the situation changed since its last block (a different tree, or different items).
  An agent that did nothing in between may stop: nothing would resolve by blocking again.
- Claude Code has its own cap of 8 consecutive continuations; guard stays far below it.

### Destroy: no cap

`destroy` blocks every time and also gates `SubagentStop`, `TaskCompleted` and `TeammateIdle`. For the last two
the block is exit code 2 with the reason on stderr, which is how Claude Code defines them (heavyq's usual exit 2
= usage does not apply to hook commands). It repeats the brief inside every block, so a long loop keeps its
thread. The only exits: proof, `item block`, `item release`, `HEAVYQ_STEER=off`.

Claude Code ends a turn after **8** consecutive Stop-hook blocks unless `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP` is
higher (`0` disables the cap; the count resets whenever the agent calls a tool). To use destroy as meant:

```
$ heavyq steer destroy
steer: destroy (stored in /work/app/.git/heavyq/steer)
Claude Code ends a turn after 8 Stop-hook blocks in a row; destroy has no cap of its own, so lift the native one:
  export CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=0
  or in the settings:  "env": {"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"}
  or let heavyq write it:  heavyq steer destroy --apply [--scope local|project|user]
```

`--apply` merges `"env": {"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"}` into the Claude Code settings file of the
scope (default `local`, `.claude/settings.local.json`), keeps every other key, writes `<file>.bak` first, and is
idempotent (`already set`). It refuses a file that is not JSON or whose `env` is not an object, and leaves it
alone. heavyq does not remove the line when you go back to `guard` or `off`: guard caps itself at 3, so a raised
native cap is harmless.

### Claim vs ledger

If the last assistant message **says the tests pass** (two phrases only: "all tests pass" and "tests pass", with
their `passed`/`passing` forms; a negation, a failure word or a conditional in the same sentence cancels it), and
the ledger shows a **plain failure** (not flaky, not under load) of the main gate, or of the proof of an item the
lane holds, **on this tree**, and it was not red at the session's start, heavyq **continues once**: a Stop
`additionalContext` (not an error, no block) with the fact and the command:

```
[heavyq · tool data] The last message says the tests pass, but the ledger shows `go test ./...` failed on this tree (exit 1).
Next: heavyq gate run ready
```

Once per session, never while `stop_hook_active` is set, and a block takes precedence. A claim the ledger cannot
confirm is not a contradiction: a missing run proves nothing either way.

### Fail open

Any doubt prints nothing and exits 0 (the reason is in `hook.log`): not a repository, no session id (nothing could
be counted), a broken config, unreadable state, a panic, a missed budget (5 seconds). With steering off the hook
costs one `git rev-parse`. Session records (`<git common dir>/heavyq/steer-sessions/<session>.json`, mode 0600)
hold the baseline, the block count, the last situation and the canary; a damaged one reads as "no memory".

## Delivery canary

Claude Code once silently dropped `additionalContext` (anthropics/claude-code#96193). So each brief the hook
sends ends with `canary: hq-<8 hex> (delivery check, ignore)`, and the token is stored with the transcript path
the hook was given. **`heavyq doctor`** looks for the latest token in that transcript:

```
brief delivery: ok (hq-3f9a1c2e reached the transcript)
brief delivery: hq-3f9a1c2e is not in the transcript: the SessionStart context may not arrive (the transcript can lag a few seconds; otherwise this Claude Code version drops additionalContext)
brief delivery: not checked (no usable transcript path)
```

Best effort and informational: it never changes doctor's exit code. The path comes from a hook input, so it is
read only when it is an absolute path to a regular `.jsonl` file, at most 256 MiB.

## Installing

`heavyq hook install claude --features queue,brief,stop` (default: `queue` only). The features are the **exact
set** heavyq owns in that settings file: installing a smaller set removes the hooks of the others, and foreign
hooks are never touched. `uninstall` removes all of heavyq's. See [claude-code.md](claude-code.md) for the
events, matchers and the plugin.

| Feature | Events | Command |
|---|---|---|
| `queue` | `PreToolUse` (Bash) | `heavyq hook claude-pretooluse` |
| `brief` | `SessionStart` (`startup\|resume\|compact`) | `heavyq hook claude-sessionstart` |
| `stop` | `Stop`, `SubagentStop`, `TaskCompleted`, `TeammateIdle` | `heavyq hook claude-stop [event]` |

`claude-stop` takes the event as an optional argument, so one command serves all four; without it the hook
input's `hook_event_name` decides (default `Stop`).

For other hooks, `internal/hook/claude_hook_io.go` holds what they share (lenient input, the output objects)
and `internal/cli/hook_handler_runner.go` the shell (bounded stdin, a budget, fail open); a feature is one row of
the table in `internal/hook/claude_features.go`.

## Limits

- Claude Code only, for now. The other agents' stop semantics differ (see the hook documentation of each agent).
- A claimed item is judged on the work tree of the lane that holds it. Two agents in one work tree are one lane.
- The gate looks at the lane's items and the main gate, not at whether the agent's *claim* in prose is complete.
- Guard's cap and `stop_hook_active` make it a nudge that a determined agent can wait out; destroy is the mode
  that insists.
