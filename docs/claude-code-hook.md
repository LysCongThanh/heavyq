# Zero-config for agents (Claude Code hook)

Parallel agents are the reason this tool exists. You shouldn't have to teach each one a wrapper.

```bash
heavyq hook install claude                    # this project, just you: .claude/settings.local.json
heavyq hook install claude --scope project    # this project, committed: .claude/settings.json
heavyq hook install claude --scope user       # every project: ~/.claude/settings.json
heavyq hook install claude --print            # show the merged settings, write nothing
heavyq hook install claude --features queue,brief,stop   # also the SessionStart brief and the stop gate (docs/steer.md); add ,stance for stances (docs/stances.md)
heavyq hook uninstall claude
```

The installer adds one `PreToolUse` hook with matcher `Bash`. It keeps everything else in the file, writes a
`.bak` first, and running it again changes nothing. A settings file that is a symlink (a dotfiles checkout)
stays one: the file it points to is updated, and a link that points nowhere is refused. From then on, when an agent runs `go test ./...`,
Claude Code passes the call to `heavyq hook claude-pretooluse`, which returns an `updatedInput`:

```
go test ./... 2>&1 | tail -30
  ->  heavyq 4 --lane wt-api --max-wait 300s -- go test ./... 2>&1 | tail -30
```

- **What gets wrapped.** By default: `npm`/`pnpm`/`yarn` `test`, `run test`, `run build` and `run gate`;
  `run typecheck` and `run lint` as `--light`; `go test`, `go build ./...`, `pytest` (also `python -m pytest`),
  `cargo test` and `cargo build`, `make test` and `make check`, `gradle`/`./gradlew test`, `mvn`/`./mvnw test`.
  Rules match whole words from the start of the command, so `npm run test:e2e` and `echo go test` don't
  match. Add your own with `[[hook.rule]]`. Each rule sets a priority, light vs heavy, a fixed lane and
  `cache = true`. A rule's `--cache` is dropped for a command with a `VAR=x` assignment the cache key can't
  see (one not listed in `cache.env`), so `GOFLAGS=-race go test ./...` never replays a plain run's result.
- **Lane.** The rule's `lane`, else `$HEAVYQ_LANE`, else the name of the git work tree containing the
  agent's cwd. One worktree per agent gives one lane per agent.
- **Shapes it understands.** `[cd DIR &&] [VAR=x ...] CMD ARGS [2>&1] [| tail|head|grep ...]`. Commands with
  `VAR=x` assignments are pinned with `--remote local`, because environment variables don't travel to
  remotes. The assignments stay in front, so they apply to heavyq too: a command that assigns `PATH`,
  `HOME`, `TMPDIR`, `XDG_*`, `HEAVYQ_*`, `CLAUDE_CONFIG_DIR` or an agent-detection variable (`CLAUDECODE`, ...)
  is left alone, since it would change how the shell finds heavyq or which queue, config and ledger heavyq
  uses. So is a command whose program PATH can't find: it's probably an alias or a shell function, and
  heavyq runs programs without a shell. Also left exactly as typed: `;`, `||`, other `&&` chains, a
  background `&`, `$(...)`, backticks, redirections, several pipes, commands already wrapped, and any call
  made while `HEAVYQ_ACTIVE` is set (heavyq sets it for the commands it runs).
- **Session.** The hook also leaves Claude Code's `session_id` in `<queue dir>/sessions/<lane>` (JSON
  `{session_id, at}`, written atomically, mode 0600), so history says which session ran what. The command
  itself stays the same in every session. A run on that lane uses the id when it has no `--session` or
  `$HEAVYQ_SESSION` and the file is under 10 minutes old; stale files are pruned with dead tickets. An id
  that isn't a plain tag (`A-Za-z0-9._:-`, 1-80 characters) is not written. Two sessions on one lane within
  those 10 minutes can swap labels, which only mislabels the session: proof matches on the tree.
- **Timeouts.** The hook adds `--max-wait` (`hook.max_wait`, default 5m, at least 1s, whole seconds
  rounded up) and raises the Bash tool's `timeout` by the same amount, so queue time doesn't use up the tool
  timeout. The base is the call's `timeout`, else Claude Code's default (`BASH_DEFAULT_TIMEOUT_MS`, else 2
  minutes). Claude Code caps the timeout (`BASH_MAX_TIMEOUT_MS`, else 10 minutes), and the hook never writes
  more than that: a call that already asks for about the cap has no room left, so its queue time comes out of
  its own time, and `additionalContext` says so. A `run_in_background` call is not held to those foreground
  limits: it gets its own `timeout` plus the wait, or keeps Claude Code's background default (about 30
  minutes) when it set none. Exit 75 still means "busy, retry later". The agent is told
  so through `additionalContext`.
- **It fails open.** Bad input, a broken config, a panic: the hook prints nothing and exits 0, so Claude
  Code runs the original command. The problem goes to `~/.local/state/heavyq/hook.log`, together with every
  rewrite and the reason a command a rule matched was left alone (secrets redacted, like everywhere else). A
  bug in heavyq must never block an agent.

**Honest limits** (details and doc quotes in [docs/claude-code.md](claude-code.md)):

- Permissions are evaluated against the *rewritten* command. By default heavyq approves nothing, so
  `Bash(npm test:*)` doesn't cover `heavyq 4 --lane api --max-wait 300s -- npm test`. Approve it at the
  prompt, allow that exact prefix, or set `hook.permission = "allow"`. That setting auto-approves only the
  commands your rules matched, and only in the shapes a `Bash(<rule>:*)` prefix rule would cover: an
  assignment Claude Code doesn't strip (`GOFLAGS=...`, `LD_PRELOAD=...`) or a `cd` out of the agent's cwd
  gets the normal prompt. Don't allow `Bash(heavyq:*)`.
- The rewrite puts heavyq in front of the program word, so an alias or shell function for that word in the
  agent's shell no longer applies. A name PATH can't find is left alone (see above); an alias that only adds
  flags to a real program (`alias make='make -j8'`) is silently skipped. Write the flags out, or add a
  `[[hook.rule]]` for the real command.
- If another `PreToolUse` hook also rewrites Bash commands, Claude Code's docs don't say whose rewrite
  wins. Don't combine them.
- Only Claude Code for now. Adapters for other agents are open issues.
- The hook was verified with Claude Code 2.1.316 (`claude -p`: the agent ran `go test ./...`, heavyq queued
  it, and the second call was a cache hit).

**A second, optional hook** (`heavyq hook claude-posttooluse`, on `PostToolUse` and `PostToolUseFailure`) adds one
short note after a repeated identical failure and measures whether it helps: see [mentor.md](mentor.md).

**Without the hook**, you can still put a rule in `CLAUDE.md` ("run heavy commands as
`heavyq <prio> --lane <worktree> --max-wait 9m -- <command>`"), or block unwrapped commands with
[`examples/claude-code/require-heavyq.sh`](../examples/claude-code/require-heavyq.sh), which makes the agent
rerun them itself.
