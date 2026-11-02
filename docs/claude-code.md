# Claude Code hook: what it relies on

`heavyq hook claude-pretooluse` is a Claude Code `PreToolUse` command hook for the `Bash` tool. It
rewrites heavy commands (`npm test`, `go test ./...`, ...) into `heavyq <prio> --lane <worktree> ... -- <command>`,
so agents don't need to know heavyq exists.

This page lists the documented behaviour the hook relies on, quoted from the official reference
(<https://code.claude.com/docs/en/hooks.md>, read on 2026-11-01), and what was verified by running the
real `claude` binary (Claude Code 2.1.316).

## Install as a Claude Code plugin

The repository is also a Claude Code marketplace (`.claude-plugin/marketplace.json`, name `heavyq`) with one
plugin (`plugins/heavyq`). It gives you the hook and a skill in two commands, no settings file edited by hand.
The binary is **not** bundled: install it first (`go install github.com/LysCongThanh/heavyq@latest` or a
[release](https://github.com/LysCongThanh/heavyq/releases)), then in Claude Code:

```
/plugin marketplace add LysCongThanh/heavyq
/plugin install heavyq@heavyq
```

or from a shell: `claude plugin marketplace add LysCongThanh/heavyq && claude plugin install heavyq@heavyq`.

What the plugin contains:

- `hooks/hooks.json`: the same `PreToolUse` hook (matcher `Bash`, timeout 30 s) that `heavyq hook install claude`
  writes, run as
  ```
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-pretooluse || exit 0'
  ```
  plus the hooks of the `brief` and `stop` features ([steer.md](steer.md)), each behind the same guard (timeout
  10 s, matcher `startup|resume|compact` for SessionStart):
  ```
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-sessionstart || exit 0'
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-stop || exit 0'
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-stop SubagentStop || exit 0'
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-stop TaskCompleted || exit 0'
  sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-stop TeammateIdle || exit 0'
  ```
  Without `heavyq` on `PATH` every hook exits 0 and prints nothing, so a teammate who has the plugin but not the
  binary sees no error and no change. With it, the stop-family hooks do nothing until you run `heavyq steer guard`
  (the default mode is off), and the SessionStart hook is silent in a repository with no anchor goal, no items and
  no red gate.
- `skills/heavyq/SKILL.md`: when to use `heavyq gate status`, `gate run`, `why` and `help agents`, what exit
  75 and 78 mean, and never to bypass heavyq.

The PreToolUse hook behaves exactly as described below (`hook.*` config, rules, fail-open). The plugin adds no `bin/`
directory and sets no version, so Claude Code treats each commit of the repository as an update.

**Plugin or settings, not both.** Hooks from all sources run in parallel, and when two of them rewrite the same
command the reference does not say which rewrite wins (see "Several hooks"). `heavyq hook install claude` therefore
reads the user, project and local settings files, and when any of them has `"enabledPlugins": {"heavyq@heavyq": true}`
it refuses with exit 2:

```
heavyq: the Claude Code plugin heavyq@heavyq is enabled in <file>, and it already runs the same hook; ...
next: keep the plugin (nothing to do), or disable it (/plugin disable heavyq@heavyq) and run this again; --force installs anyway
```

`--force` installs anyway (and `--print` and `uninstall` are never refused). If you installed through the CLI first
and now want the plugin, run `heavyq hook uninstall claude` for each scope you used before enabling it.

The plugin files are checked in CI by Go tests (`internal/hook/claude_plugin_files_test.go`): the JSON
structure, the required fields, that every event, matcher, timeout and command equals the installer's feature
table, and the skill's frontmatter and size. Locally,
`claude plugin validate .` and `claude plugin validate plugins/heavyq` give the authoritative answer. They warn
about a missing `version` and `icon`, which is intended.

## SessionStart and Stop hooks: what they rely on

`heavyq hook install claude --features queue,brief,stop` (or the plugin) adds the hooks behind
`heavyq brief` and `heavyq steer` ([steer.md](steer.md)). Quoted from the same reference (read on 2026-11-02):

- **SessionStart** matchers `startup`, `resume`, `clear`, `compact` and `fork` say how the session began; heavyq
  registers `startup|resume|compact`. Output: `{"hookSpecificOutput": {"hookEventName": "SessionStart",
  "additionalContext": "..."}}`, "String added to Claude's context at the start of the conversation, before the
  first prompt". `additionalContext`, `systemMessage` and plain stdout are each capped at 10,000 characters
  (heavyq clips at 9,500 and keeps the brief under 1,150 bytes). The input has `source`, `transcript_path` and
  `session_id`. "Claude's first response still waits for the hooks to finish": heavyq's budget is 5 s.
- **Stop** input: `stop_hook_active` ("`true` when Claude Code is already continuing as a result of a stop hook.
  Check this value ... to avoid blocking on a condition that will never resolve") and `last_assistant_message`.
  Output: `{"decision": "block", "reason": "..."}`, or `hookSpecificOutput.additionalContext` with
  `hookEventName` `Stop`: "non-error feedback ... The conversation continues so Claude can act on it", shown as
  "Stop hook feedback" and counted by the same loop protections. heavyq blocks with `decision` and uses
  `additionalContext` for the claim-vs-ledger continuation.
- **The cap.** "Claude Code applies an 8-consecutive-continuation cap: after stop hooks have continued the turn
  eight times in a row, Claude Code overrides the next block and ends the turn. The count resets each time Claude
  calls a tool." `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP` ("default: 8. Set to `0` to disable the cap") raises it: guard
  (3 blocks) never needs it, destroy does ([steer.md](steer.md), `heavyq steer destroy --apply`).
- **SubagentStop** has the same decision control and `stop_hook_active`, `agent_id`, `last_assistant_message`.
- **TaskCompleted and TeammateIdle** have no matchers. "Exit code 2: the task is not marked as completed and the
  stderr message is fed back to the model as feedback" / "the teammate receives the stderr message as feedback
  and continues working instead of going idle". heavyq (destroy mode only) blocks with exit 2 and the reason on
  stderr. They have no `last_assistant_message`, so claim-vs-ledger does not apply.
- Failure modes are the PreToolUse ones above: another exit code, bad JSON or a timeout is a non-blocking error, and
  every heavyq hook exits 0 with no output on any doubt, except the deliberate exit 2 of a destroy block.

Golden tests (`internal/hook/testdata/{sessionstart,stop_block,stop_continue,system_message}.golden.json`) pin
the exact JSON; `go test ./internal/hook -update` rewrites them after an intended change.

## Rewriting is supported: `updatedInput`

> `PreToolUse`: `updatedInput` directly under `hookSpecificOutput` replaces a tool's arguments before it runs.

From the "PreToolUse decision control" table:

> | `updatedInput` | Modifies the tool's input parameters before execution. Replaces the entire input object, so include unchanged fields alongside modified ones. Claude Code evaluates permission rules and a Bash command's auto-background eligibility against the input your hook returns, not the input Claude sent. Combine with `"allow"` to auto-approve, or `"ask"` to show the modified input to the user. For `"defer"`, ignored |

What heavyq does with this:

- It copies every field of `tool_input` (`description`, `run_in_background`, ...) and replaces only `command`
  and `timeout`, because the input object is replaced as a whole.
- It raises `timeout` by `hook.max_wait`, so time spent waiting in the queue doesn't use up the Bash tool's
  timeout. The reference says of the Bash `timeout`: "Values above the maximum are reduced to the maximum
  rather than rejected".
- It adds `additionalContext` (documented as "String added to Claude's context alongside the tool result")
  that tells the agent why the command was queued and what exit 75 means.

## Permissions: heavyq does not approve anything by default

> | `permissionDecision` | `"allow"` skips the permission prompt [...] `"deny"` prevents the tool call. `"ask"` prompts the user to confirm. `"defer"` ends the turn so the tool can be resumed later [...] Deny and ask rules are still evaluated regardless of what the hook returns |

By default heavyq returns `updatedInput` **without** a `permissionDecision`. The docs don't spell this case
out, so it was tested with Claude Code 2.1.316 in `-p` mode:

- The rewrite is applied. A hook that returned only `updatedInput` changed `echo hello` into another
  command, and that other command ran.
- The rewritten command goes through the normal permission flow. With only `Bash(echo:*)` allowed, a hook
  that rewrote `echo hello` into `python3 -c ... && echo ...` was refused: "This Bash command contains
  multiple operations. The following part requires approval: python3 -c ...".

So permission rules see `heavyq 4 --lane api --max-wait 300s -- npm test`, not `npm test`. A rule such as
`Bash(npm test:*)` no longer matches it. You have two options:

1. Approve the wrapped command at the prompt, or allow exact prefixes such as
   `Bash(heavyq 4 --lane api --max-wait 300s -- npm test *)`. The lane is the work tree's name, so each
   tree's prefix is stable. The prefix is also the same in every Claude Code session, because the session id
   does not travel in the command (below).
2. Set `hook.permission = "allow"`. heavyq then returns `"allow"` for the commands it rewrites. These are
   only commands that matched a `[[hook.rule]]` or a built-in rule. This auto-approves them, including
   `npm test`, which runs whatever `package.json` says. Turn it on only if you would allow those commands
   anyway. Deny and ask rules still apply.

The Claude session id reaches the ledger out of band. When the hook wraps a command it writes
`<queue dir>/sessions/<lane>` (JSON `{"session_id": ..., "at": ...}`, atomic, mode 0600), and `heavyq <prio>`
reads it when it has no `--session` and no `$HEAVYQ_SESSION`, if the file is under 10 minutes old and the id is a
valid tag. Two Claude sessions on the same lane within that window can swap labels in history; that only
mislabels the session, since proof matches on the tree.

Don't use a blanket `Bash(heavyq:*)` allow rule: it would approve `heavyq 4 -- <anything>`. Don't put a `*`
in the middle either. The permissions reference says "A `*` in a Bash rule matches any text, including spaces", so
`Bash(heavyq 4 --lane * -- npm test)` also matches `heavyq 4 --lane x -- sh -c '...' -- npm test`.

## Input fields used

> In addition to the common input fields, PreToolUse hooks receive `tool_name`, `tool_input`, and `tool_use_id`.

The reference's Bash example input includes `"cwd"`, `"hook_event_name": "PreToolUse"`, `"tool_name": "Bash"`
and `"tool_input": {"command": "npm test", "description": "...", "timeout": 120000, "run_in_background": false}`.
heavyq reads `cwd` (to name the lane after the git work tree), `hook_event_name`, `tool_name` and
`tool_input.command` / `tool_input.timeout`.

## Exit codes, failures and timeouts: why the hook fails open

> **Success**: your hook exits 0. Claude Code applies any JSON output fields your hook printed, and the action goes ahead unless those fields block or deny it.

> Exit code 0 with no output means the hook has no decision to report, so the tool call continues through the normal permission flow.

> **Non-blocking error**: your hook exits with any other code, or fails in some other way, such as not starting or printing invalid JSON. The action goes ahead [...]

> On `PreToolUse`, a timed-out `command`, `http`, or `mcp_tool` hook doesn't block the tool call [...]

> Defaults: 600 for `command`, `http`, and `mcp_tool` [...]

heavyq always exits 0. It prints JSON only when it rewrites a command. For anything else (a command it
doesn't recognise, a shape it can't parse safely, unreadable input, a broken config, a panic) it prints
nothing. The original command then runs unchanged, and the problem is logged to
`$XDG_STATE_HOME/heavyq/hook.log`. `heavyq hook install` sets `"timeout": 30` for the hook. The hook only
parses a string and may call `git rev-parse` once, so a stall is unlikely, and per the docs a timeout
wouldn't block the call anyway.

## Several hooks

> When multiple PreToolUse hooks return different decisions, precedence is `deny` > `defer` > `ask` > `allow`.

> When an event fires, its matching hooks run in parallel.

> Hook entries merge across settings levels rather than replacing each other.

If another `PreToolUse` hook also returns `updatedInput` for the same call, the reference doesn't say which
rewrite wins. Don't combine heavyq's hook with another hook that rewrites Bash commands.

## Where `heavyq hook install claude` writes

> | `~/.claude/settings.json` | All your projects | No, local to your machine |
> | `.claude/settings.json` | Single project | Yes, can be committed to the repo |
> | `.claude/settings.local.json` | Single project | No, gitignored when Claude Code saves a setting to it |

`--scope user`, `--scope project` and `--scope local` (the default) map to these three files. `--features` picks
which hooks to write (`queue` by default; `brief`, `stop`): exactly that set, so a smaller set removes the hooks
of the others. The project root is
the git top level of the current directory. `$CLAUDE_CONFIG_DIR` replaces `~/.claude` when set. The
installer keeps every other key, writes `<file>.bak` before changing anything, replaces the file
atomically, and is idempotent. It refuses (and leaves the file alone) if the file isn't valid JSON or
`hooks` (or an event heavyq registers on, such as `hooks.PreToolUse`) has an unexpected type. `heavyq hook uninstall claude` removes only entries
whose command is `<binary> hook claude-<name> [event]` for a hook some feature installs (a heavyq hook you wrote by hand, such as the mentor's `claude-posttooluse`, is left alone).

With `--scope project`, teammates who don't have heavyq installed are not blocked. For them the hook fails
to start, which the docs classify as a non-blocking error, so their commands run unchanged.

## Dogfood run (2026-11-01, Claude Code 2.1.316)

The test used a temporary Go module in a git repo named `wt-dogfood`, with the hook installed at project
scope and a `[[hook.rule]] match = ["go test"], cache = true`. It ran:

```
claude -p 'Use the Bash tool to run exactly `go test ./...` ... then run it a second time ...' --allowedTools 'Bash(heavyq:*)'
```

(The broad allow rule was only for the throwaway repo.) Result:

- Both tool calls had `"command": "go test ./..."`. The agent never typed heavyq.
- `hook.log`: `rewritten: heavyq 4 --lane wt-dogfood --cache --max-wait 120s -- go test ./...` (twice).
- First tool result: `heavyq: our turn after 0s (prio 4, lane wt-dogfood, local/heavy-1): go test ./...` then
  `ok  example.com/dog 0.003s`. History: `miss`, run 7 s.
- Second tool result: the same `ok` line, then `heavyq: cached result from 2026-11-01 20:50:10 (local)`.
  History: `hit`, run 0 s.

To repeat it by hand: install the hook in a scratch repo, start `claude`, ask it to run the test suite,
then check `heavyq history` and `~/.local/state/heavyq/hook.log`.
