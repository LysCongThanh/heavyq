# heavyq statusline: know it is on track without asking

`heavyq statusline` prints heavyq's segment of Claude Code's status line: how far the work is, whether anything
needs you, and when it should be done. It is for reassurance, not alarms. You work at your machine all day; the
segment lets you plan ahead, switch task or go to sleep knowing that either it is `on track` or heavyq told you.

```
◆ 5/8 proven  ⚑ 1 needs you  ▲ CI wait  ✓ on track · ~16:40      something to act on or look at
◆ 5/8 ✓ ~16:40                                                   nothing notable: almost silent
◆ 5/8 proven  ⚠ at risk · ~17:15                                  the forecast says it will be late
◆ 8/8 ✓ done                                                     everything is proven
◆ 1/8                                                            too few proofs to forecast yet
(nothing)                                                         no flow state, or no items and nothing waiting
```

- `◆ 5/8` is proven items out of all items (a ticked box is not proof; see [items.md](items.md)).
- `⚑ 1 needs you` is something only a person can supply: a blocked item's question, a permission prompt, a
  credential. It is red.
- `▲ CI wait` is the constraint, shown only when it is significant (it limits the flow, not just a share of it).
- `✓ on track · ~16:40` and `⚠ at risk · ~17:15` are the [forecast](#the-forecast), with the clock time it ends
  (with the weekday when it is not today).
- At most 60 columns, one space after every glyph, no emoji. `NO_COLOR` drops the colour and keeps bold;
  `--ascii` (or a locale that is not UTF-8, or `TERM=dumb`) replaces the glyphs with `# ! ^ + .`. Parts drop in a
  fixed order when the line would be too wide: the constraint first, then the word `proven`.

## Set it up

Claude Code has one `statusLine`. In `~/.claude/settings.json` (or the project's `.claude/settings.json`):

```json
{ "statusLine": { "type": "command", "command": "heavyq statusline" } }
```

That replaces any status line you have. To **keep yours**, wrap it: `--wrap` runs your command with the same stdin
and appends heavyq's segment to the last line of its output, behind a `│`. Your command is not edited, copied or
reimplemented; heavyq only runs it the way Claude Code would have (`sh -c`, 5 s at most). Past 5 s the command is
killed with everything it started (it runs in its own process group), so a slow refresh leaves nothing behind.

```json
{ "statusLine": { "type": "command", "command": "heavyq statusline --wrap 'node \"$HOME/.claude/statusline.cjs\"'" } }
```

That is the shape for ClaudeKit's status line. Its layout is config-driven (`statuslineLayout` in `.ck.json`) but
only over its own built-in sections (model, context, quota, directory, git, cost, changes, agents, todos): it has no
external-command section, so the wrapper is the way to add heavyq. Remove the wrapper and the original command is
back; nothing of ClaudeKit was touched. The exit code of the wrapped command is passed through, and with nothing
to say the segment is left out and its output is untouched.

Claude Code can also refresh the line on a timer while a session is open (`refreshInterval` in `statusLine`; see
its documentation), which keeps `~16:40` and the staleness check moving when nothing else redraws it.

## What it will not do: the budget

Claude Code debounces the status line at 300 ms, and a slow script blocks updates. So the command:

- reads two small files and nothing else: the status-line JSON on stdin (`cwd`, `workspace.current_dir`,
  `model.id`, `version`, `session_id`, `rate_limits`) and `<git common dir>/heavyq/flow/state.json`;
- finds the repository by walking up to `.git` (it follows a linked work tree's `commondir`), **never runs git** and
  never computes flow;
- takes well under 5 ms in the process itself (a test holds it; the Go binary's own start-up is on top);
- when the state is older than 60 s, starts a detached `heavyq flow refresh` (at most once per 15 s) and prints
  what it has; where this build has no `flow` command, nothing is started;
- gives up on a stdin that never closes after 200 ms, and never fails: any problem leaves the segment out.

Nothing is written in a repository where heavyq has no flow state.

## The forecast

`ETA = now + remaining items × the recent p50 claim-to-proof ÷ running lanes` (at least one lane), from the flow
state's `throughput` (the same numbers `heavyq recap` shows). When no item was ever claimed and then proven there is
no p50 (proofs by a lead who never claimed): the measured rate stands in, `ETA = now + remaining items ÷
proven_per_hour`, and the rate already counts the lanes. It is `unknown` (nothing is shown) until three finishes have
been counted (every finished item counts, claimed or not: see [flow.md](flow.md#the-aggregates)) and, without a p50,
a rate has been measured (at least a minute of active time). It is `at risk` when

- the ETA is past the run's expected end (a run declared by `heavyq preflight`), or
- nothing has been proven for over twice the p50 (twice the typical gap, 1 ÷ the rate, without a p50) while lanes
  are working (needs `throughput.last_proof` in the state; the recap derives it from the items instead).

Otherwise it is `on track`. Confidence is labelled by the number of finishes: 3 to 5 low, 6 to 11 medium, 12 or more
high (`heavyq recap` prints it). The code is `internal/forecast`, a pure function with a table test.

## Notifications

The same call decides whether something is worth a push, and says it **once per state change**, never again for the
same state:

| When | Message |
|---|---|
| a new thing needs you | `needs you: doc-7: permission_prompt: Bash(npm run e2e)` |
| the forecast turns at risk | `at risk: ETA is 15m past the expected end, ETA ~17:15` |
| every item is proven | `all 8 items proven` |

Several changes in one evaluation become one message. The budget is 5 pushes per local day
(`HEAVYQ_NOTIFY_BUDGET=N`, `0` turns pushing off); a change the budget swallows is not pushed later. A state older
than 10 minutes never pushes, and a repository that is already finished when first looked at stays quiet.

Channels are local, the first that works wins: `notify-send` (Linux, with a desktop session) or `osascript`
(macOS), then the terminal: OSC 9 (iTerm2, WezTerm, Ghostty, Windows Terminal), OSC 777 (foot) or the bell, written
to `/dev/tty`. Over ssh the desktop is not yours, so only the terminal is used; under tmux or screen it gets the bell,
which always travels. `HEAVYQ_NOTIFY=off|terminal|desktop` picks. The message goes to the notifier as arguments, never through a shell, with control characters removed and
commands redacted. `--no-notify` turns it off for one call. The record is
`<git common dir>/heavyq/notify/state.json`; `heavyq recap` runs the same check. No webhook: engineers are at their
machine.

## Session record

The status line is the only place the agent's version and the quota windows are visible, so it leaves a small
record per session, `<git common dir>/heavyq/sessions/<session id>.meta.json` (model, agent version, five-hour and
seven-day usage, model change points), written by `internal/sessionmeta` only when something changed and pruned
after 14 days. The ledger joins its `agent` and `model` fields on it, and the flow state shows the newest one
([flow.md](flow.md)).

## Developing

`Segment(state, now, theme)` is pure; the golden segments (idle, busy, needs you, at risk, done, unknown, empty and
no state, each in `NO_COLOR` and ASCII) are in `internal/statusline/testdata/golden`. After an intended change:
`go test ./internal/statusline -update`, then read the diff. The flow state is read with `flowstate.ReadFile` into
the `flow.Snapshot` that writes it (one set of types, no second parser); `internal/flowview` keeps the file-system
repository finder and the forecast adapter, which takes `throughput.last_proof` for "at risk" by stall.
