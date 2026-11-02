## What and why

<!-- One topic per PR. Link the issue: "Fixes #123". -->

## How it was tested

<!-- A test that fails without this change, or the commands you ran. -->

## Checklist

- [ ] PR title is a conventional commit (`feat:`, `fix:`, `docs:` ...); it becomes the squash commit
- [ ] `go test -race ./...` and `golangci-lint run` pass
- [ ] User-visible change: docs (README, `docs/`: flags, config table, exit codes) updated, and a changelog fragment added (`changelog.d/<slug>.<kind>.md`; do not edit `CHANGELOG.md`)
- [ ] Exit codes 2, 75, 76, 77, 78, 255 keep their meaning; the hook still fails open
