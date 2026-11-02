**Parallel pull requests stop conflicting, and CI is tiered.** A pull request no longer edits `CHANGELOG.md`
(it adds `changelog.d/<slug>.<kind>.md`; `go run ./tools/changelog release <version>` folds them at release time),
the usage text of `heavyq --help` is generated from the command table, and a command registers its table entry
and its dispatch from its own `<name>_command_spec.go`, so a new command edits no shared file. Pull requests run
the fast checks only; macOS, the release snapshot, the install script, govulncheck, the full race suite and the
long fuzz run on pushes to `main`, nightly, and on a PR labelled `ci:full`.
