**Runs go only where their tools are.** Before a ticket is queued, heavyq asks each candidate remote whether the
command's argv[0] (seen through `env`, `sh -c '...'` and the gate wrapper) and every tool named with the new
`--tools a,b` flag or a gate's new `tools = [...]` key resolve on the PATH a job gets there (`env_path` included), and
skips a remote that lacks one: `heavyq why` and `heavyq status` say `box: lacks envsubst (see its env_path)`. An
explicit `--remote` without the tool exits 2 before any code is shipped, naming the tools and `env_path` (an agent gets
the usual `NOT RUN` digest), so an environment gap no longer turns a gate red. Answers are cached per remote in the state directory (found: 1 h,
missing: 2 min). `heavyq doctor` lists which common tools (`go`, `node`, `npx`, `pnpm`, `python3`, `cargo`, `docker`,
plus the gates' `tools`) resolve on each remote, and an exit 127 on a remote names that remote and its `env_path`.
See [docs/remote.md](docs/remote.md#placed-where-its-tools-are---tools).
