# Gates: `heavyq.toml`

A repository describes what "proven" means for it in `heavyq.toml` at its repository root (the work tree
top level; in a linked worktree, that worktree's own copy). A **gate** is a named list of shell commands that
must all pass on the current code.

> Status: this page describes the file format and its validation. `heavyq gate run` and `heavyq gate status`,
> which use the file, are not released yet. Today the loader (`internal/repoconfig`) reads and validates
> the file and nothing executes it.

## Trust model

`heavyq.toml` is trusted like a **Makefile**: it holds commands, and whoever can edit it decides what they
are. Because of that:

- Gates run only on an explicit `heavyq gate run`. Hooks never execute gates.
- The file is parsed strictly. Unknown keys, wrong types and out-of-range values are errors, never
  ignored. A typo must not silently weaken a gate.
- Commands are run through `sh -c`, so quoting, pipes and `&&` work as in a shell.

## Format

```toml
[[gate]]
name   = "ready"                       # required
run    = ["gofmt -l . | (! grep .)",   # required, non-empty, run in order
          "go vet ./...",
          "go test -race ./..."]
prio   = 3                             # optional, 1-9, default 3
light  = false                         # optional, may use the light slots
remote = "local"                       # optional: local (default) | auto | <remote name>

[anchor]                               # reserved: accepted, not used yet
file  = ".heavyq/anchor.md"
extra = ["plans/*/phase-*.md"]
```

| Key | Type | Default | Rules |
|---|---|---|---|
| `gate.name` | string | required | `[a-z0-9][a-z0-9_-]{0,39}`, unique in the file |
| `gate.run` | list of strings | required | non-empty list; each string non-blank, at most 2000 bytes, no NUL |
| `gate.prio` | integer | `3` | 1-9; 1 runs first |
| `gate.light` | bool | `false` | |
| `gate.remote` | string | `"local"` | `local`, `auto`, or a remote name (`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`). Whether the remote exists is checked against your own config when the gate runs. |
| `anchor.file` | string | none | relative path inside the repository |
| `anchor.extra` | list of strings | none | relative globs inside the repository |

The file may be empty (no gates). It is at most 1 MiB and must be a regular file (a symlink to one is fine).

## Errors

Every error names the file. Unknown keys also name the line when it can be found:

```
/work/app/heavyq.toml:8: unknown key "gate.light_job"
/work/app/heavyq.toml: [[gate]] #2 ("go"): prio 10 must be between 1 and 9
/work/app/heavyq.toml: [[gate]] #3: duplicate gate name "ready"
```

A directory that is not in a git work tree, or a repository without `heavyq.toml`, simply has no gates. That
is not an error.
