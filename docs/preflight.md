# Preflight: front-load the human before an unattended run

A long run (a harness turn, a workflow, a night) mostly produces nothing for two reasons: a question
surfaces at 1 a.m. when nobody is there, or a permission prompt nobody answers blocks the run. `heavyq
preflight` asks those questions **now**, while the human is present, and says GO or NO-GO.

```
$ heavyq preflight --hours 8
preflight: NO-GO (3 to handle now, 2 to watch)
goal: ship login v2
plan: 2 items, 3 lanes, 8 h
1. [anchor] item docs has no proof: it can only be ticked by hand, never proven -> edit .heavyq/anchor.md:5: add `proof: <command>` to docs
2. [dependencies] item ui needs a human: design sign-off -> answer it, then delete the `needs:` span at .heavyq/anchor.md:4
3. [permissions] `go test ./auth/...` may prompt: the heavyq hook rewrites it to `heavyq 4 --lane api --max-wait 300s -- go test ./auth/...` and no allow rule covers that (or set hook.permission = "allow") -> edit .claude/settings.local.json: add "Bash(heavyq 4 --lane api --max-wait 300s -- go test *)" to permissions.allow
4. (watch) [environment] 3 lanes planned but 2 heavy slots in all (local 2): the rest queue behind them -> heavyq preflight --parallel 2
5. (watch) [permissions] an unattended `claude -p` run hangs on a prompt nobody answers; deny instead, so a missing rule is a failure you can read -> claude -p --permission-prompts none "<prompt>"
```

```bash
heavyq preflight                      # GO or NO-GO; opens a run span on GO
heavyq preflight --hours 8            # also the span's expected end, and the length quota is judged for
heavyq preflight --parallel 4         # lanes planned (default: lanes holding leases, else 3)
heavyq preflight --start              # open the span even on NO-GO
heavyq preflight --json               # the document below
heavyq run current | ls | end [<id>]  # the run spans
```

Exit: **0** GO, **1** NO-GO (or the span could not be written), **2** usage (a flag out of range, not inside a git
work tree). In agent mode (`docs/agent-mode.md`) the output ends with one `next:` line: the fix of the first
blocking finding, or `heavyq item claim --next` on GO.

## What it checks

A finding is a **block** (the verdict is NO-GO; a human must act) or a **watch** (listed, the verdict stays GO).
Blocks come first. Each finding ends with `-> <what fixes it>`: a command, or an `edit <file>:<line>` hint.

| Check | Blocks | Watches |
|---|---|---|
| **anchor** | no goal; no items; nothing left (every item proven); an open item without `proof:`; a proof that cannot run (no such gate, or its first program is not installed) | no known duration for a proof (it never passed here: the run length is a guess); a proof longer than the 30 min lease (only a heavyq run renews a lease); proofs that alone take longer than `--hours` across the lanes |
| **dependencies** | an item blocked by `item block` or a `needs:` span (a question for the human); an `after:` id that is not an item; an `after:` loop | |
| **environment** | under 1 GiB free on the repository's file system | under 5 GiB free; a configured remote that did not answer a 3 s probe; more lanes than heavy slots (local plus reachable remotes); a pool with fewer values than lanes |
| **permissions** | a command that is denied, or surely prompts, in Claude Code (below) | a command that may prompt but the matcher is not sure; the headless advice when any may prompt |
| **quota** | | 5 hour window at 80 % or more (or 50 % with a run over 5 h); 7 day quota at 85 % or more (or 70 % with a run of 12 h or more) |

*Duration* is the median `run_s` of the passing runs of the proof's command in the ledger (by `cmd_hash`; a
gate is the sum over its commands), from the last 30 days. *Items the run works on* are the open ones: not
finished (proven and ticked) and not waiting on a human.

`after:` is an optional code span on the item line, `` `after: a, b` ``, read by preflight from the raw line
(the anchor parser ignores it); a repository that does not use it passes that part of the check.

### Permissions

Skipped, with a note, when there is no sign of Claude Code: no `.claude/settings.json`,
`.claude/settings.local.json` or `~/.claude/settings.json` (`$CLAUDE_CONFIG_DIR/settings.json` when set), and
`$CLAUDECODE` is unset. Otherwise, for every proof command of an open item (a gate stands for its commands)
plus `heavyq item done --run`, it evaluates the command the way Claude Code does:

1. `defaultMode = bypassPermissions`, or a bare `Bash` / `Bash(*)` allow rule: nothing can prompt.
2. If the heavyq PreToolUse hook is installed (a settings file runs `hook claude-pretooluse`, or the plugin is
   enabled) and `hook.enabled` is not false, the command goes through the real hook decision (`hook.Decide` with
   your `[hook]` rules). When it wraps the command, permission rules see `heavyq 4 --lane api --max-wait 300s
   -- go test ./...`, not `go test ./...` ([claude-code.md](claude-code.md), "Permissions"). With
   `hook.permission = "allow"` the hook approves it; otherwise an allow rule must match the wrapped command.
   The fix names that exact prefix, never a blanket `Bash(heavyq:*)`.
3. The command is cut at `&&`, `||`, `;`, `|`, `&` and newlines (outside quotes) and every part must pass. A leading
   `timeout`, `time`, `nice`, `nohup`, `stdbuf`, `command`, bare `xargs`, and an assignment of a variable Claude Code
   strips (`CI=1`, `NODE_ENV=test`, `GOOS=linux`, ...) are removed before an `allow` rule is matched; a `deny` or `ask` rule
   matches past any of them. A part is then judged by, in order: a `deny` rule, an `ask` rule, a redirection to a file
   (`> out.txt` asks unless an `Edit` rule allows it), an `allow` rule (`Bash(go test:*)` and `Bash(go test *)` are a prefix
   on a word boundary and also match the bare command; any other `*` is any text; otherwise exact), and the built-in
   read-only commands (`ls`, `cat`, `git status`, `find` without `-exec`/`-delete`, ...). A command with `$( )`, backticks or a
   dangling `&&` can only be covered by an exact rule.

**Block or warn.** The check blocks (NO-GO) only when it is sure: a deny or ask rule matches, or no allow rule can match
and nothing built in covers the command. What it cannot decide is a *warning*, `may prompt`, and the verdict stays GO:

| Shape | Why it is only a warning |
|---|---|
| a leading `VAR=value` assignment not known to be stripped (`GOFLAGS=...`) | Claude Code's list of strippable variables is not documented |
| a part ending in `&`, a `( )` subshell, a `for`/`while`/`if` construct | Claude Code was seen to ask for some although the rules match, and to allow others |
| a command that may be built in as read-only (`tree`, `jq`, `awk`, ...), or a read-only one with an unquoted glob | the built-in set is larger than the documented part |
| a redirection to a file when an `Edit` rule or `acceptEdits` exists | the file rules are not read |

**Checked against Claude Code.** The matcher is not trusted from the documentation alone.
`internal/preflight/testdata/permission-corpus/corpus.json` holds commands with the decision a live headless Claude Code
made for them under given allow, deny and ask rules (and the heavyq hook rewrite), recorded with `claude -p
--permission-prompts none --output-format stream-json`; the file names the Claude Code version (2.1.316) and says
`"source": "recorded"`. `TestPermissionMatcherCorpus` fails when the matcher disagrees with a case. A case the matcher can
only warn about carries `may_warn` with the reason, and the test insists it warns. To re-record, run
`python3 -I internal/preflight/testdata/permission-corpus/record.py [ruleset ...]` (needs a logged-in `claude`; it uses the
cheapest model and costs a fraction of a cent per ruleset) and read the diff: a changed decision is Claude Code changing.
A headless run ignores a project's settings until its folder is trusted, so the recorder passes the rules with `--settings`.

The checks stay conservative where it is cheap: a false alarm costs one allow rule, a miss costs the night. For
unattended `claude -p` runs add `--permission-prompts none`, which denies instead of hanging (Claude Code
2.1.259 and later); the rules still matter, or the denials become failures.

### Quota

Optional. When a status line wrote `<git common dir>/heavyq/sessions/*.meta.json` with
`rate_limits.five_hour_pct` / `seven_day_pct` (percent used) within the last hour, the newest one is read. No
file, an old one or a damaged one means "unknown", and nothing is said. The thresholds are heuristics, not a
forecast.

## JSON (`--json`, schema 1)

```json
{
  "schema": 1,
  "verdict": "NO-GO",
  "goal": "ship login v2",
  "hours": 8,
  "parallel": 3,
  "items": ["api"],
  "checked": ["anchor", "dependencies", "environment", "quota"],
  "skipped": ["permissions: no Claude Code settings found (...)"],
  "findings": [
    {"check": "anchor", "severity": "block", "subject": "ui",
     "text": "item ui has no proof: it can only be ticked by hand, never proven",
     "fix": "edit .heavyq/anchor.md:3: add `proof: <command>` to ui"}
  ],
  "span": null
}
```

`hours` is 0 when not stated. `severity` is `block` or `warn` (the text output calls a warning "watch"). `span`
is the run span that was opened (below) or `null`. Findings are blocks first, then warnings, each in the order the checks
ran. Commands in findings go through the redactor.

## Run spans

A **run span** is the plan of one long run: what preflight checked, kept so that later readers (recap, the flow
state, the brief) can compare plan and actual. One JSON file per span, `<git common dir>/heavyq/runs/<id>.json`
(mode 0600, atomic writes, shared by every work tree, never committed):

```json
{"id":"20261102-220000-ab12","started":"2026-11-02T22:00:00Z","expected_end":"2026-11-03T06:00:00Z",
 "goal":"ship login v2","items":["api","docs"],"parallel":3,"host":"workstation"}
```

`expected_end` is `null` without `--hours`. `heavyq run end` adds `"ended"`. Opening a span ends any span still
open (a repository runs one thing at a time). A span that was never ended stops counting as current an hour
after its expected end, or 24 hours after it started when it has none.

| Command | What it does |
|---|---|
| `heavyq run end [<id>]` | close the current span (or `<id>`); exit 1 when none is open |
| `heavyq run ls [--json]` | every span, newest first, `open`, `lapsed` or `ended` |
| `heavyq run current [--json]` | the open span; exit 1 (`null` with `--json`) when none |

`heavyq run` is not the priority form (`heavyq 3 -- ...`); `run` followed by anything but these three words is
the usual "first argument must be a priority 1-9 or a command" error. In the command table the entry is
`runspan`, with `run` as an alias.

For Go code, `internal/runspan` is the whole interface: `Current(common, now)`, `Open`, `End`, `List`, `Get`.

## The brief's nudge

When the SessionStart brief is built for a lane that has items, has **no open run span** and has run at least 5
heavy commands in the last 30 minutes (replays of cache hits and runs that never ran do not count), it adds one
line, `next: heavyq preflight`, after the brief and the stance. It says it at most once per session (a marker
file under `<git common dir>/heavyq/preflight-nudged/`), and never without a session id.
