# AGENTS.md

Instructions for coding agents (Claude Code, Codex, Cursor, ...) working **on** the heavyq repository.
Humans: [CONTRIBUTING.md](CONTRIBUTING.md) has the same rules with more context.

## Project in one paragraph

heavyq is a single Go binary (Linux first, macOS supported without cgo) that queues heavy shell commands by priority,
gates them on RAM/load, can offload them to other machines over SSH by exact commit, caches results by git
tree, and ships a Claude Code `PreToolUse` hook plus a loopback web dashboard. No daemon: the queue is a
directory of ticket files and `flock(2)` slot files. Package map: see CONTRIBUTING.md, "Where things live".

## Commands

```bash
go build ./...
go test -race -count=1 ./...      # the gate; spawns real processes, locks and temp git repos (~1 min)
go test ./internal/hook -update   # only when hook output intentionally changes; review the golden diff
golangci-lint run                 # v2; config in .golangci.yml
gofmt -l .                        # must print nothing
```

If heavyq itself is installed, run the gate through it like any other agent would: `heavyq gate run ready`
(`heavyq.toml`: gofmt, vet, race tests; proven commands are skipped, and `heavyq gate status` says whether
this exact code is proven). CI runs the same gate.

## Hard rules

- **No new dependencies** without an issue first. Stdlib plus `BurntSushi/toml` only.
- **Files stay small** (~200 lines) with descriptive `snake_case.go` names. Split rather than grow.
- **Hot paths never scan the ledger**: hooks, `gate status`, `why` and the Tower read `internal/ledgerindex`
  (index first, a bounded `history.ReadTail` as fallback) and write records with `ledgerindex.Append`.
  See `docs/ledger.md`; the 20 ms hook budget is a test.
- **Tests use real things**: real processes, real locks, real temp git repos. No mocks of the OS unless a
  real process cannot produce the case.
- **Fail closed** on unsafe or unknown input (config values, remote names, refs, paths). Never guess and run.
- **The hook fails open**: on any doubt `heavyq hook claude-pretooluse` exits 0 with no output. New command
  shapes need table tests in `internal/hook/claude_pretooluse_test.go`.
- **Exit codes are API**: 2, 75, 76, 77, 78, 255 keep their meaning.
- **Agent output is a budget** (`docs/agent-mode.md`): a run prints at most queued, running and a digest; the
  digest stays under 4 KB, `why` under 600 bytes, `heavyq help agents` under 900. Never print ANSI in agent
  mode. E2E tests run the binary without `CLAUDECODE` and friends unless they test agent mode.
- **Dashboard security model** (`docs/dashboard.md`): loopback only, token, `Host`/`Origin` checks, CSP
  `default-src 'none'`. Do not loosen any of these. Keep the page free of external resources.
- **Redaction**: anything that shows command lines goes through `internal/redact`.
- **One command table**: a new command, subcommand or flag goes into `internal/cli/command_table_*.go` (it feeds
  `--help`, completions and the man page) and into `usage`; drift tests fail otherwise. A change to a config key,
  a validation rule or the status snapshot needs `cd tools/schemagen && go run .`, with `schema/` committed.
- **Docs move with behaviour**: flags, config keys and exit codes change in README.md, docs/ (configuration.md, exit-codes.md) and CHANGELOG.md
  (Keep a Changelog) in the same PR.

## Commits and PRs

- Conventional commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `ci:`, `build:`); the PR title
  is checked and becomes the squash commit.
- One topic per PR, with a test that fails without the change.
- Never commit secrets, host names or personal paths. CI runs gitleaks on every push.
