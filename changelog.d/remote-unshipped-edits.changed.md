**A remote run says when uncommitted edits are not shipped.** A run admitted to a remote while the work tree has
uncommitted changes to tracked files, or untracked files that git does not ignore, prints one line with the count, so a
pass on the shipped files is not taken for a pass on the files being edited:
`heavyq: warning: 3 uncommitted files are not shipped: buildbox runs the committed tree only (commit them, or use --remote local)`.
In agent mode it is the end of the running line, so the three-line budget holds. A local run, `--ref` other than `HEAD`,
a clean tree and ignored files say nothing, and hooks, `why` and `gate status` are untouched.
See [docs/remote.md](docs/remote.md).
