# Flow: where work waits

`heavyq flow` answers "how is it going, and what is holding it up?" without anyone declaring anything. Every
state is **derived** from facts heavyq already keeps: the run ledger, the item leases and their events, the
derived item status and what the Claude Code hooks observed. A source that is missing (no anchor, no hooks)
simply drops its states; nothing breaks.

```
heavyq flow show            # the materialised state, as text (a few lines; needs-you first)
heavyq flow show --json     # the state document itself (schema below)
heavyq flow refresh         # recompute now, ignoring the 60 s debounce
```

`show` only reads `<git common dir>/heavyq/flow/state.json`, in a few milliseconds. A repository with no state yet
gets one computed, once.

## States

A **timeline** is the sequence of states of one work item (an anchor line), or, for runs no item claimed, of one
session (else one lane). A run belongs to the item named by its ledger tag (`--item`, `HEAVYQ_ITEM`), else to the
item its lane held when it started, so a minute is never counted twice.

| State | Derived from |
|---|---|
| `queued` | the run's `wait_s` in the ledger |
| `working` | a lease is held; for sessions, runs and the short silences (up to 10 minutes) between them |
| `proving` | runs tagged with a gate, or whose command is the item's proof (`run_s`) |
| `rework` | from a failed proof to the next proof attempt |
| `blocked_human` | `heavyq item block`; a permission or idle prompt, input wanted or a stopped turn (hooks, below) |
| `ci_wait`, `merge_wait`, `conflict_rework` | gh and git, **optional**: see "Collectors: git and gh" |
| `done_proven`, `claimed_unproven` | end a timeline (no duration): the item's derived status says so |

Where spans overlap the highest wins: `blocked_human`, `proving`, `conflict_rework`, `ci_wait`, `merge_wait`,
`rework`, `queued`, `working`. Silence longer than 10 minutes belongs to no state (the agent was away, not flowing).

## The aggregates

All over a rolling window, the last four hours of wall-clock time (`window_s`; a preflight run span replaces it
later).

- **Fleet lead time** is the sum of all timeline spans in the window, by state. **Flow efficiency** is
  `working / lead time`.
- **Throughput** is finished items per active hour (the wall-clock union of all spans). An item is finished when
  its proof passed or it was marked done inside the window, **claimed or not**, and **whether or not the code has
  changed since**: an item that reads `stale` now was finished then. The traces are the item reading `proven`, a
  `done` event, and, for an item that reads `stale`, its gate passing in the ledger (every command the gate has now,
  on one tree). A claimed, open or blocked item is never finished by a pass of its gate: on a gate several lanes
  share, another lane's pass on its own tree is not this item's proof. A cache replay or a coalesced run is not a
  pass either, so a pass whose original run left the lookback, or ran on a remote, is undercounted (the safe
  direction). An item counts once however often it was proved, `n` is that count, and `last_proof` is the newest
  finish. The median time from a claim to the finish that followed it (`p50_claim_to_proof_s`) is the lead time: it
  uses claim-to-finish pairs only (the first finish after the item's last claim, not the first one in the window)
  and is `0` when no item has one, which never keeps an item out of the count. Without pull requests "proven" is
  the proof, not the merge.
- **The constraint** is the state with the largest share of lead time, excluding `working` (that is the work itself;
  its share is the flow efficiency). `significant` is true when the share is at least 0.30 **and** above the state's own
  baseline: an exponentially weighted average (alpha 0.2) of the share that state held in past windows, folded in at most
  once per 30 minutes and kept in `state.json` (`baseline`). The baseline starts empty, so a new wait is news at once; a wait
  that persists is learned as normal over hours, and a large but normal share is no longer flagged.

## `state.json`

```json
{"schema":1,"repo":"/work/app","updated":"2026-11-02T10:40:00Z",
 "goal":"ship login v2",
 "run":null,
 "items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":0,"done_stale":0,"open":0},
 "needs_you":[{"item":"doc-7","lane":"docs","session":"sess-1","reason":"permission_prompt: Bash(npm run e2e)","since":"2026-11-02T10:31:00Z"}],
 "constraint":{"state":"ci_wait","share":0.41,"significant":true},
 "throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9,"last_proof":"2026-11-02T10:35:00Z"},
 "lanes":[{"lane":"api","state":"proving","item":"login-api","since":"2026-11-02T10:35:00Z"}],
 "session":{"model":"claude-opus-5","agent_version":"2.1.90","rate_limits":{"five_hour_pct":61,"seven_day_pct":20}},
 "fleet":{"lead_s":5400,"by_state_s":{"working":3000,"ci_wait":2200},"flow_efficiency":0.55},
 "events":[{"at":"2026-11-02T10:20:00Z","kind":"stop_failure","lane":"api","session":"sess-1","detail":"rate_limit"}]}
```

Other tools (the status line, `top`, recap) read this file in parallel, so the shape is a contract: a field may be
added, never renamed or retyped.

- `run` is `null` unless a run span is open (`internal/runspan`, from `heavyq preflight`); then it is
  `{"id","started","expected_end"(or null),"goal","items"(count),"parallel"}`. `session` is `null` until a session meta record exists.
- `baseline` (`{"at","shares":{"<state>":share}}`, additive, optional) is what the constraint finder learned is normal.
  Delete the file to forget it.
- `constraint.state` is `""` when there is no lead time. `needs_you` and `lanes` are `[]` when empty.
- `throughput.last_proof` is when the newest item in the window was proven, `null` when none was (with `p50_claim_to_proof_s`
  it tells "nothing proven for twice the median while lanes work", the at-risk signal).
- `items.open` counts items nobody holds and nothing proves (a stale one nobody ticked included).
  `claimed_unproven` counts items ticked that nothing ever proved. `done_stale` (additive) counts items ticked whose
  proof passed on older code: the code changed since, so they need one more proof run (`heavyq item done <id> --run`),
  and they are neither `open` nor `claimed_unproven`.
- `needs_you[].item` is the item the lane holds, `""` when none. `since` is when the lane started waiting.
- `fleet` and `events` were added after the first draft of the contract (additive). `events` holds the newest 10
  stop failures and model switches in the window: `kind` is `stop_failure` (`detail` is Claude Code's `error`:
  `rate_limit`, `overloaded`, ...) or `model_switch` (`detail` is the new model).
- `quota` is `null`, or the one fleet-level entry a usage-limit stop becomes (additive):
  `{"since","last_hit","agents","lanes":[...],"window":"five_hour","resets_at":"..."|null}`. A StopFailure `rate_limit`
  or a Notification `quota_auto_resume_*` opens it; four agents that hit the 5-hour limit within minutes of each
  other are `agents: 4`, and `needs_you` carries **one** entry for them (`lane: "fleet"`, reason `quota hit at
  19:20, 4 agents stopped, resets ~23:20`), not one per lane. `window` and `resets_at` come from the status line's
  `rate_limits` (`resets_at` is null when it never said). It closes on the first of: any session's next tool call, a
  ledger run or lease past a 2 minute grace after the last stop (`QuotaGrace`; agents mid-turn keep finishing tool calls
  for a moment), a `quota_auto_resume_*` notice that says agents are resuming, the status line showing the window reset
  (its reset time passed, or its newest report has every window under 100%), or 24 hours. Quota stops no longer count as
  `blocked_human` time: nobody is being waited on, the account is.
- `session` also carries `updated` (when the status line last reported) and `rate_limits.five_hour_resets_at` /
  `seven_day_resets_at` (additive).

### When it is written

At most once every 60 seconds, atomically (temp file and rename) and under a `flock`, so many hooks and runs at
once produce one computation and one write; readers never see half a file. The natural events:

- the end of any `heavyq run` (`HEAVYQ_FLOW=off` stops it);
- the `SessionStart` and `Stop` hooks (the brief and the stop gate: whatever features are installed);
- the `PermissionRequest`, `Notification` and `StopFailure` hooks of the `flow` feature.

**Never at `PreToolUse`**, the hot path. A needs-you event (a person is needed, or an answer arrived) forces the
write: such events are rare and must show within the event that caused them, not within a minute.
`heavyq flow refresh` forces it too.

## Hooks: observing where an agent waits

```
heavyq hook install claude --features queue,flow      # or the plugin, which carries it
```

`--features flow` registers one observer, `heavyq hook claude-flow`, on `PermissionRequest`, `Notification`
(matcher `permission_prompt|idle_prompt|agent_needs_input`), `StopFailure`, `PostModelSwitch` and `PostToolUse`.
It **only observes: it never returns a permission decision**, so a permission stays the person's to give or deny.
It appends to `<common>/heavyq/flow/events.jsonl` and fails open like every heavyq hook.

| Event | Effect |
|---|---|
| `PermissionRequest` | the lane is `blocked_human` now, reason `permission_request: Bash(<command>)` |
| `Notification` `permission_prompt` / `idle_prompt` / `agent_needs_input` | the same, reason `<type>: <message>` (`permission_prompt` follows a `PermissionRequest` about six seconds later and adds no second entry; the stronger reason stays) |
| `Notification` `quota_auto_resume_*` | a usage-limit stop (the `quota` entry above); one whose type ends in `_started`, `_resumed`, `_done`, `_complete` or `_succeeded` says agents are resuming and lifts it |
| other notifications (`auth_success`, ...) | ignored: nobody is needed |
| `StopFailure` | an `events` entry. `rate_limit` opens the `quota` entry; any other error (`overloaded`, ...) makes the lane wait: `stop_failure: overloaded` |
| `PostModelSwitch` | a `model_switch` change point, and the session's model is updated |
| `PostToolUse` | clears the session's block (whatever lane it was recorded under) and, once past the grace, the fleet's `quota`: the agent is working again |

A block also ends at the next ledger run or lease of its lane **or of its session** (a session that moved between
work trees is one agent, so a block recorded under one lane clears when it runs under another), and one nobody answered
for 24 hours is dropped (the session is gone). The `SessionStart`, `UserPromptSubmit` and `Stop` hooks count as activity
too. A lane whose work tree no longer exists (the newest work tree the ledger or a hook recorded for it is gone from
disk) drops out of `lanes` and `needs_you`; a lane with no recorded work tree is never dropped. Command lines in reasons go
through `internal/redact` and are clipped. `PostToolUse` fires after every tool call, so its handler costs two failed
reads when nothing is blocked and no quota is open: a flag file per session and one for the fleet, in heavyq's state
directory, no git call. The usage limit is the account's, so its flag is one file for every repository: activity in
another repository lifts it in all of them. `HEAVYQ_FLOW_QUOTA_GRACE` (a duration, default `2m`) overrides the grace of
the hooks.

## Agent and model in the ledger

Run records gain optional `agent` (`claude-code/2.1.90`) and `model` (`claude-opus-5`), joined from the session's meta
record when the run is written, so baselines can be segmented at a change point. No hook carries the agent version and
`SessionStart.model` is optional; the status line's stdin JSON always has `model.id` and `version`. So:

```
heavyq hook claude-sessionmeta     # reads status-line JSON on stdin; prints nothing
```

stores `<common>/heavyq/sessions/<session>.meta.json` (model, version, `rate_limits.five_hour/seven_day` used
percentages, change points). It skips the write when nothing changed in the last minute, so a 300 ms status-line
cadence costs a small read. In Go the same call is `sessionmeta.RecordStatusline` (`internal/sessionmeta`), for the
status line command to use in-process.

## Collectors: git and gh

A collector returns spans for an item (`flow.External`) and they are painted into its timeline like any other.
They live in `internal/flow/collect`, which is the only place that runs git or gh; `internal/flow` stays pure. A
branch or pull request belongs to the item whose id its name carries: `feat/flow-collectors` is the item
`flow-collectors`, the longest id wins. A repository with no anchor items collects nothing.

- **`conflict_rework` (git, local, works offline).** For every branch named after an item, the merge commits that
  merged the default branch (origin's HEAD, else `origin/main`, `main`, ...) in and conflicted. The conflict is
  found by redoing the merge (`git merge-tree`, git 2.38 or newer; older git reads the merge's combined diff and can
  miss a conflict settled by taking one side). Git knows when a merge was made, not how long it took: the span
  ends at the merge and starts at the branch's previous commit, between one and 15 minutes before it. A clean merge of main is no
  rework. The conflicted files are kept (`flow.ConflictHotspots` ranks them: one file in at least 30% of the conflicts is a
  structural hotspot, not bad luck).
- **`ci_wait` and `merge_wait` (gh, optional).** `ci_wait` is every workflow run on an item's branch, from its start
  to its end (to now while it is going): each push and each rerun is a run, so a convoy of reruns shows as one.
  `merge_wait` runs from the moment the newest commit's runs were all green until the pull request was merged (until now while
  it is not); drafts and closed pull requests wait for nothing.

gh is used only when it is on the PATH and logged in, and it **fails open**: offline, unauthenticated, slow (3 s) or a
repository not on GitHub, it yields nothing, or the spans of its last good answer, and never fails the refresh.
The two calls (`gh pr list`, `gh run list`) are cached in `<common>/heavyq/flow/gh-cache.json` and made **at most
once per five minutes**, a failed call included. Spans still open are cut five minutes after the last good answer, so stale data
never claims a wait that has since ended. `HEAVYQ_FLOW_GH=off` turns gh off; git stays on.

## Not in this version

- **Item names from agent branches.** A branch is matched to an item by name; an agent's work tree branch
  (`worktree-agent-...`) names no item, so its merges and runs are not attributed.
- **The run span as the window.** `run` is filled, but the aggregates still use the last four hours; a run span
  replacing the window is the next step.

## Files

| Path (under `<git common dir>/heavyq/`) | What |
|---|---|
| `flow/state.json` | the materialised state |
| `flow/gh-cache.json` | the last answer of gh and when it was asked |
| `flow/events.jsonl` (`.1`) | hook observations, append-only, rotated at 512 KiB |
| `flow/state.lock` | the flock |
| `runs/<id>.json` | run spans (`internal/runspan`) |
| `sessions/<session>.meta.json` | model, agent version, rate limits |

Nothing is committed: it all lives under the git common dir, shared by every work tree of the repository.
