# Agent mode

Most heavyq calls come from coding agents, and every byte of output costs them context. Agent mode is
what heavyq says when the caller is an agent: a few lines instead of progress chatter, and a bounded
**digest** at the end of the output with the verdict, the lines that matter and the one thing to do next.
Humans keep the output they have today.

## When it is on

Resolved once per process, first match wins:

| Rule | Result |
|---|---|
| `HEAVYQ_OUTPUT=agent` / `human` / `json` | that mode |
| `HEAVYQ_AGENT=1` (`true`, `yes`, `on`) / `0` (`false`, `no`, `off`) | agent / human |
| an agent variable from the table below is set (not `""`, `0` or `false`) | agent |
| otherwise | human |

A bad value of `HEAVYQ_OUTPUT` or `HEAVYQ_AGENT` is ignored with one `heavyq: warning:` line, and the next
rule decides.

| Variable | Set by | Evidence |
|---|---|---|
| `CLAUDECODE` | Claude Code | verified on 2.1.316 |
| `AI_AGENT` | Claude Code (`claude-code_<version>_agent`) and others that follow the convention | verified on 2.1.316 |
| `GEMINI_CLI` | Gemini CLI | integration research |
| `COPILOT_CLI` | GitHub Copilot CLI | integration research |
| `CODEX_SANDBOX`, `CODEX_CI`, `CODEX_THREAD_ID` | OpenAI Codex | undocumented; names used by third-party detectors |
| `CURSOR_AGENT` | Cursor agent | undocumented; name used by third-party detectors |

The table is `agentmode.AgentEnv` in `internal/agentmode/mode_detect.go`. Names that also configure a
tool for humans (`CODEX_HOME`, `CURSOR_TRACE_ID`) are left out on purpose. An agent that none of these
identifies (Codex and Cursor document nothing) sets `HEAVYQ_OUTPUT=agent`. Being on a pipe is not enough: CI and
`| tee` would turn it on.

The Claude Code hook needs no change: the rewritten command is the same text (so permission rules keep
matching), and the Bash tool's environment already has `CLAUDECODE=1`.

## What a run prints

At most three lines of heavyq's own, around the command's output:

```
heavyq: queued #3 of 4 on local (p3, lane wt-api); ahead: wt-a(p2) wt-b(p3)      only if it waits 2 s or more
heavyq: running go test ./... (p3, lane wt-api, local/heavy-1, tree 3f2a9c1d7b0e, waited 2m5s)
<the command's own output, byte for byte>
heavyq: PASS go test ./... · tree 3f2a9c1d7b0e · 41s (waited 2m5s)
```

- **The running line opens the output.** Claude Code keeps only about 10,000 characters (head and tail)
  of a failing command's output, so the first line and the digest are the parts that survive.
- **Edits a remote leaves behind are in the running line.** A run admitted to a remote with uncommitted
  tracked changes or untracked files that git does not ignore ends the running line with
  `, 3 uncommitted files not shipped` (before the closing parenthesis): the remote runs the committed tree, so a
  pass says nothing about those files. It is part of the line, not a line of its own, so the budget below holds.
  See [remote.md](remote.md).
- **Waiting is quiet.** The first progress line comes after 2 s of waiting; a heartbeat
  (`heavyq: still queued #2 of 4 after 5m0s ...`) follows at 5 minutes and every 5 minutes after. An admission
  within 2 s says nothing but the running line. The heartbeat starts at 5 minutes, not 60 s: a 60 s one makes a
  2-minute wait print four lines, over the budget below.
- **No ANSI, ever.** heavyq never colours its own output (so `NO_COLOR` has nothing to switch off) and
  strips escape sequences from every line it quotes, the command line included: a newline, carriage return,
  tab or escape byte in argv (a multi-line `sh -c` script) becomes a space, so each line stays one line. A
  command's own colours pass through untouched, as before.
- **Nothing else.** Notes a person gets as lines of their own (a `sh -c` pipe hides a failing first stage, a
  remote job's banner, a remote's setup error in full) are in the digest for an agent, or left out.
- A cache hit or a coalesced result replaces the "cached result from ..." sentences with the digest.

## The digest

`--digest` prints it for a person too. It is written to **stderr**, after the command has ended and its
output has been fully written, so it is the last thing in the stream. It is always under 4096 bytes.

```
heavyq: FAIL go test ./... · exit 1 · tree 3f2a9c1d7b0e · 12s
  --- FAIL: TestRun (0.01s)
  run_test.go:42: got 1, want 2
  FAIL
  ... +3 more failure-like lines in the output
log: /home/me/.local/state/heavyq/logs/20261102T010203.123456Z-4242-wt-api.log (full output, 212 KiB)
next: fix the failure above, then re-run this command
```

| Verdict | Meaning | `next:` |
|---|---|---|
| `PASS` | the command exited 0 | none |
| `PASS?` | it exited 0, but its output has a shell "command not found" line (`sh: 1: tsc: not found`, `bash: line 1: tsc: command not found`, `zsh: command not found: tsc`): usually a pipeline (`a \| tail`) whose last stage succeeded. The second line quotes the offending line. The run is recorded as `suspect` (never as proof) and not cached. `--strict`, gates and item proofs make it `FAIL` with exit 127 | install the missing tool where it ran, or make the pipe fail (`bash -o pipefail -c`), then re-run |
| `FAIL` | it exited non-zero (header shows `exit N`) | fix, then re-run (`... then run: heavyq gate run <name>` under `--gate`); 127 and 126 say what is wrong; 255 from a remote says the connection was lost (a remote command's own 255 is reported as 96); a remote run whose output says `origin/<branch>` is missing there says to re-run with `--base-ref origin/<branch>` ([remote](remote.md)) |
| `REUSED` | the result of an identical run on this exact tree was replayed | none when it passed: the second line says `already passed on this exact tree at <time> (lane X); do not re-run`. A replayed failure says `already failed (exit N) ...` and ends with `next: change the code, then re-run this command` |
| `NOT RUN` | heavyq gave up before the command ran: `--max-wait` (75), remotes down (76), remote setup failed (77; a command that exits 77 itself is a `FAIL`), the work tree changed while waiting or the queue itself failed (2) | 75: `retry later; don't bypass heavyq`. 76 and 77: retry later or add `--remote local`. 2: ``run it again; if that repeats, `heavyq why` explains the queue`` |
| `CANCELLED` | an operator cancelled it (78) | `cancelled by operator; ask before re-running` |
| `INTERRUPTED` | a signal stopped it (128+n) | `run it again if you still need the result` |

Every verdict except `PASS` and a replayed pass ends with exactly one `next:` line, and it is the last
line of the output.

**Failure lines** come from the last 64 KiB of the command's stdout and stderr (kept in arrival order by a tee
that passes every byte on unchanged): up to 12 distinct lines, in order, that look like a failure
(`file:line`, `FAIL`, `--- FAIL`, `Error:`, `error[`, `error:`, `panic:`, `✕`, `AssertionError`,
`Traceback`). A flood of one repeated line counts once. When nothing matches, the last three non-empty lines
stand in. Each is clipped to 200 bytes.

**The log** is the full output (first 4 MiB of it), written to `logs/` next to the ledger
(`$XDG_STATE_HOME/heavyq/logs`, or beside `$HEAVYQ_HISTORY`) with mode 0600, because output can hold secrets. It
exists only for a run that did not succeed: a passing run leaves nothing. heavyq keeps the newest 30 and
deletes any older than 7 days, and only ever touches files it named itself.

**`HEAVYQ_OUTPUT=json`** keeps the terse chatter and ends with the digest as one JSON line, under the same
4096-byte cap:

```json
{"heavyq":"digest","verdict":"FAIL","cmd":"go test ./...","exit":1,"tree":"3f2a9c1d7b0e","secs":12.3,"waited_secs":0,"failures":["run_test.go:42: got 1, want 2"],"more_failures":3,"log":"/home/me/.local/state/heavyq/logs/....log","next":"fix the failure above, then re-run this command"}
```

`tree`, `why`, `detail`, `failures`, `more_failures`, `log` and `next` are omitted when empty. `tree` is 12
characters of the git tree id; a `+<8 characters>` suffix means uncommitted edits.

**A person with `--digest`:** the command's stdout and stderr become pipes (heavyq has to copy them), so a
tool that only colours a terminal stops colouring. Without `--digest` the command gets the real streams,
exactly as before.

## Other commands

- `heavyq gate status` and `gate run`: when the gate is not proven the last line is
  `next: heavyq gate run <name>` (the "Prove it with" sentence is dropped). `--json` is unchanged.
- `heavyq why [lane]` is cut to 600 bytes, whole lines first, with `... (heavyq status shows the rest)` at
  the cut.
- `heavyq fleet` prints its digest instead of the tables: one line per machine (overloaded first) and per
  queue, a `next:` line with the worker caps to apply, under 4 KB; `--json` and `--md` are unchanged
  ([fleet](fleet.md)).
- A usage error ends with ``next: fix the arguments; `heavyq help agents` shows the usage``.
- `heavyq help agents` prints a usage card of at most 900 bytes (about 200 tokens): the digest, the exit
  codes, what to do on 75 and 78, gate status and gate run, why.

## Budgets enforced by tests

| Budget | Test |
|---|---|
| a 2-minute wait says 1 line (queued); with the running line and the digest, 3 | `TestAgentTwoMinuteWaitSaysQueuedOnce` (fake clock), `TestAgentModeWaitPrintsAtMostThreeHeavyqLines` |
| a digest is under 4096 bytes, with 6 MB of failing output too | `TestDigestStaysUnderTheBudgetWithHugeFailingOutput`, `TestAgentModeFailureDigestIsBoundedAndKeepsTheFullLog` |
| `why` is at most 600 bytes | `TestWhyStaysUnderTheByteBudgetInAgentMode` |
| the `fleet` digest is under 4096 bytes, with 60 live tickets | `TestReportShapes`, `TestE2EFleetObservesEveryQueue` |
| `heavyq help agents` is at most 900 bytes | `TestCardBudget`, `TestHelpAgentsCardFitsItsBudget` |
| no ANSI in any agent output | `noANSI` in the end-to-end tests |
