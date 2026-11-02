# Mentor: break repeat failures, and measure whether it helps

An agent that sees `go test` fail will often run the identical command again on the identical code. heavyq
knows this from its ledger, so it can say so: one short note, made of facts from the ledger, after the 2nd
and 3rd identical failure. And because "it probably helps" is not evidence, the note goes to 80% of the
eligible events and is held back from the other 20%. The difference between the two groups is measured and
shown by `heavyq mentor stats`. A rule that does not help turns itself off.

The mentor never pastes command output and never runs anything. It does not claim to think like a mentor:
the claim is a measured change in behaviour.

## The rule: repeat failure

Raised when the newest run of a lane is the **2nd or later identical failure in a row**:

- the same `run_id` (same tree, argv, env values, subdirectory), so the code did not change;
- the same non-zero exit code each time;
- a deterministic failure: never **124** (timeout), **137** (kill, usually the OOM killer), **130**/**143**
  (somebody stopped it), heavyq's own **75-78** and **255**; never a run that started `under_load`
  (see [gates](gates.md)), and never a command that also **passed** on this tree (flaky);
- cache replays and runs that never ran (gave up, cancelled) are not counted; other commands run in between do
  not break the repetition; an unknown tree (no `run_id`) never fires.

2nd failure, a fact:

```
[heavyq · tool data] `go test ./...` exited 1 twice in a row on the same tree (3f9a1c0d2b7e).
```

3rd failure and later, a directive with one `Next:`:

```
[heavyq · tool data] `go test ./...` exited 1 3 times in a row on the same tree (3f9a1c0d2b7e). Next: change the code or its inputs before rerunning, or report BLOCKED with needs:.
```

One line. Every number comes from a ledger field (exit code, count, tree); the command is the ledger's redacted
`cmd`, cut to 60 characters. The 2nd failure gets a fact, the 3rd a directive with one `Next:`.

**It speaks on a state change only**, not on every call:

- a signature (the `run_id`) raises a given level once: the fact once, the directive once; a 4th identical failure
  is no new state;
- at most **3 events per rule per session and lane** (a new `session_id` starts over);
- a signature waits **3 seconds** between two events (measured on the ledger's clock);
- each ledger record is considered once, so a following `ls` does not repeat the note.

## The holdout

Every event that passes those limits is an **eligible event** and is drawn into an arm with a seeded draw:
`sha256(seed, rule, event)` modulo 100 is below `holdout_pct` (default 20) for **holdout**, else **inject**.
The seed is kept in `<state dir>/mentor/seed` (64 random bits, created on first use; `HEAVYQ_MENTOR_SEED`
overrides it), and every log line carries it, so a reader can recompute any draw.

- **inject:** the note is printed as the hook's `additionalContext`.
- **holdout:** nothing is printed. The event is logged and counts for the limits exactly like an injected one,
  so both arms see the same events.
- `holdout_pct = 100` is **shadow mode**: everything is logged and measured, the agent sees nothing.
  `holdout_pct = 0` disables the holdout (and the measurement).

### The outcome

The primary outcome of an event: **did the next heavy run differ, within 3 runs?** Differ means another
`run_id`, so another command or another tree (an edit counts). After 3 runs that all repeat the failing one the
outcome is `same`; with fewer it is `pending`; an event whose run has aged out of the lane's index is `unknown`
and counts in neither rate. A cache replay of the same run repeats it.

The hook settles the lane's pending events on its next call, and `heavyq mentor stats` settles the rest, so
numbers are current when you ask.

### The intervention log

`<state dir>/mentor/interventions.jsonl`, next to the ledger. One JSON line per eligible event:

```json
{"v":1,"at":"2026-11-02T09:41:07Z","rule":"repeat-failure","level":2,"seed":"9f3b0a77c1d2e455","arm":"holdout",
 "event":"01K8...","lane":"wt-api","session":"s-1","run_id":"...","tree":"...","streak":2,"outcome":"pending"}
```

`event` is the id of the ledger record that raised it. The log is append-only: when an outcome becomes known, the
line is written again with `outcome` (and `outcome_at`) set, and readers keep the last line per event.

## `heavyq mentor`

```
heavyq mentor stats [--json]
heavyq mentor mute <rule>
heavyq mentor unmute <rule>
```

`stats` prints, per rule and arm, the events, how many are resolved, how many differed and the rate, then the
**lift** (inject rate minus holdout rate, in percentage points) and the auto-mute policy. `--json`:

```json
{"rules":[{"rule":"repeat-failure",
  "inject":{"events":212,"resolved":198,"differed":141,"rate":0.712},
  "holdout":{"events":51,"resolved":47,"differed":22,"rate":0.468},
  "lift_pp":24.4,"muted":false}]}
```

`lift_pp` is absent until both arms have a resolved event. `mute` / `unmute` take a known rule name; an unknown one
exits 2.

### Auto-mute

A rule with a lift **below 10 points after 200 resolved events per arm** mutes itself (`muted_by: "auto"`, with
the numbers as the reason): it stops evaluating and stops logging. The check runs when an event resolves and
in `stats`. `unmute` starts the judgement over, counting only events after it. Configure it in `[mentor]`
(see [configuration](configuration.md)): `auto_mute` (default `true`), `min_events` (200), `min_lift_pp` (10),
`holdout_pct` (20). About 170 events per arm are enough to tell 50% from 65%, so 200 is the floor for a verdict.

## Wiring it up

Add the hook to Claude Code's settings. **Both events**: Claude Code reports a Bash command that exits non-zero as
`PostToolUseFailure`, one that exits 0 (a pipe into `tail` does, whatever heavyq returned) as `PostToolUse`.

```json
{
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "heavyq hook claude-posttooluse"}]}
    ],
    "PostToolUseFailure": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "heavyq hook claude-posttooluse"}]}
    ]
  }
}
```

(`heavyq hook install` does not write this yet; a `mentor` feature for the installer is planned.)

The hook reads the payload (it never reads `tool_response`), finds the lane (the command's `--lane`, else
`$HEAVYQ_LANE`, else the work tree's name, as the PreToolUse hook derives it), and reads that lane's newest
records from the [ledger index](ledger.md). It acts only when the lane's newest record ended within the last
minute. Whether or not the command Claude reports is the rewritten one, the ledger decides.

- **Output:** `{"hookSpecificOutput":{"hookEventName":"PostToolUse|PostToolUseFailure","additionalContext":"[heavyq · tool data] ..."}}`,
  only for an injected event. Otherwise nothing.
- **Fails open:** any error (bad JSON, broken config, unreadable state) or a spent budget (15 ms, `internal/deadline`)
  prints nothing and exits 0; the reason goes to `hook.log`. It never scans the ledger.
- **Escape:** `HEAVYQ_MENTOR=off` in the environment silences the hook; `heavyq mentor mute repeat-failure`
  silences the rule for good.

## Files

| File | Where | What |
|---|---|---|
| `interventions.jsonl`, `seed`, `mute.json` | `<state dir>/mentor/` (beside the ledger; with `HEAVYQ_HISTORY`, beside that file) | the log, the experiment seed, the mute state |
| `<lane>.json` | `<queue dir>/mentor/` | per lane: the last record looked at, events per session, levels per signature, pending outcomes. Runtime state: losing it only lets a rule speak again |

## Limits worth knowing

- One rule exists. The framework (a rule, the holdout, the log, the stats, the mute) is the point: other rules
  (claim against ledger at Stop, environmental-failure notice) would plug into the same log.
- The measurement is as good as its sample: the lift is a difference of two rates over events from a few lanes.
  Treat a number from fewer than 200 resolved events per arm as a hint.
- Delivery is checked by the host, not by heavyq: Claude Code has dropped `additionalContext` in some versions.
  Test the hook on each Claude Code upgrade (`heavyq mentor stats` shows `inject` events; the transcript shows the note).
