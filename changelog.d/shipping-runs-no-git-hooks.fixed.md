**Shipping a commit to a remote runs no git hook on the remote.** `git push` already skipped the repository's own
`pre-push`; the bare repo on the remote now gets `core.hooksPath=/dev/null` in its own config (also when it already
exists), so a global `core.hooksPath` or a template with hooks on that machine can no longer decline the push (exit 77),
abort `git init` through a `reference-transaction` hook, or print into the job's output through the `post-checkout` of
the job's worktree. See [docs/remote.md](docs/remote.md).
