# Changelog fragments

A pull request that changes behaviour adds **one file here** instead of editing `CHANGELOG.md`, so parallel
pull requests never conflict on it.

```
changelog.d/<pr-number-or-short-slug>.<kind>.md
```

`kind` is one of `added`, `changed`, `fixed`, `security`. The file holds the release-notes entry, in the style
of `CHANGELOG.md`: a bold lead, then what changed and why a user cares. Plain text becomes one bullet; a file
that starts with `- ` is used as written (several bullets are fine).

```markdown
**`heavyq top`.** A terminal monitor for the pane next to your agents: `heavyq top`, `--line` for tmux.
See [docs/top.md](docs/top.md).
```

CI (`pr.yml`, job `changelog`) wants a fragment on every pull request that touches `internal/` or `cmd/` code
(test files do not count). A PR that needs no entry (refactor, CI, typo) carries the label `no-changelog`.

At release time the maintainer folds the fragments into `CHANGELOG.md` and deletes them:

```bash
go run ./tools/changelog release 0.5.0 -dry-run   # print the new CHANGELOG.md, change nothing
go run ./tools/changelog release 0.5.0            # fold, then delete the fragments
```

Entries land under `## [0.5.0] - <today>` in the order Added, Changed, Fixed, Security, after anything
`[Unreleased]` already holds; the compare links at the bottom move with it.
