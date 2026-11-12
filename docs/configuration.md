# Configuration

`--config FILE`, else `$HEAVYQ_CONFIG`, else `$XDG_CONFIG_HOME/heavyq/config.toml` (`~/.config/heavyq/config.toml`).
Every key is optional; keys this heavyq does not know are ignored with a warning. [`examples/config.toml`](../examples/config.toml) has every key with comments.
A duration, `load_weight_mb`, `max_load_ratio` or `*_mb` budget set to `0` means its default; a negative one is an error that names the key.
The remote-only keys (`ssh`, `transport`, `work_dir`, `git_url`, `cache_hook`, `env_path`, `name`, `observe_only`) are an error under `[local]`, as `legacy_lock_files` is under `[[remote]]`.

| Key | Default | Meaning |
|---|---|---|
| `queue.poll` | `"2s"` | how often a waiting ticket retries (at least `"50ms"`) |
| `queue.force_after` | `"15m"` | head ticket waiting this long skips the load check (RAM must still fit); only for a ticket with one candidate machine |
| `queue.resource_gate` | `true` | `false` = slots only |
| `queue.auto` | `true` | score-based placement for tickets without `--remote` |
| `queue.load_weight_mb` | `500` | `k` in `score = free_mb - k x load1` |
| `queue.hold_for` | `"1m"` | how long a fresh job's RAM budget stays reserved |
| `queue.remote_down_max` | `"10m"` | remote-only ticket exits 76 after its remotes are unreachable this long |
| `queue.probe_timeout` | `"5s"` | timeout of one remote metrics probe |
| `queue.base_ref` | `""` | default `--base-ref`: `remote/branch` (e.g. `"origin/main"`) is recreated on the remote machine a run goes to and its commit is part of the run id; `""` = origin's default branch (`refs/remotes/origin/HEAD`, else `origin/main`) when the repository has one; `"none"` = no base ref. See [remote](remote.md) |
| `local.heavy_slots` / `light_slots` | `2` / `1` | slots on this machine; each absent key gets its own default, an explicit `0` is kept. At least one machine needs a heavy slot: heavy runs (the default) cannot use light ones |
| `remote.heavy_slots` / `light_slots` | `2` / `0` | slots on that remote, when neither key is set (`light_slots = 2` alone is a light-only remote). Each machine has at least one slot and at most 64 |
| `local.legacy_lock_files` | `[]` | use these files as heavy slots (shared with `flock <file>` users): absolute paths, each with its own base name; `light_slots` then defaults to `0` |
| `*.max_load_ratio` | `0.75` | admit while `load1 < ratio x cores`; at most 64 |
| `*.job_mb` / `light_mb` / `reserve_mb` | `3072` / `1024` / `2048` | RAM budget per heavy / light job, and headroom; `0` = the default |
| `remote.name`, `remote.ssh` | required | name (`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`, unique, not `local` or `auto`, which `--remote` reads as keywords) and a plain ssh alias, options in `~/.ssh/config` (`ssh` may be replaced by `transport` + `git_url`) |
| `remote.transport` | `["ssh","-o","BatchMode=yes","-o","ConnectTimeout=10","-o","ServerAliveInterval=15","-o","ServerAliveCountMax=4",ssh]` | argv prefix; the remote script is appended as one argument. A custom ssh transport should keep the `ServerAlive*` options: they end the local side of a run about a minute after its link dies silently (the remote side needs `ClientAliveInterval` in its `sshd_config`, see docs/remote.md) |
| `remote.work_dir` | `".heavyq"` | remote directory, relative to the remote home (a leading `~/` means the same; any other `~` is refused) |
| `remote.git_url` | `"<ssh>:<work_dir>/git"` | where bare repos are pushed (never starts with `-`) |
| `remote.cache_hook` | `""` | shell run in the job's checkout before the command; gets `HEAVYQ_CACHE_DIR`, `HEAVYQ_WORKTREE`. A cancel or a dropped connection stops it at once, like the command |
| `remote.env_path` | `""` | directories put first on the remote command's PATH, `:`-separated, `~/` = remote home. See [a spare Mac](remote.md#a-spare-mac-as-a-remote). Placement checks a run's tools on that same PATH and skips a remote that lacks one ([`--tools`](remote.md#placed-where-its-tools-are---tools)) |
| `remote.allow_untrusted_exit` | `false` | when the remote fails its [exit canary](remote.md#the-exit-canary-a-remote-that-loses-exit-codes-proves-nothing), still run commands that prove nothing there (recorded as suspect); proofs are refused either way. Not valid in `[local]` |
| `remote.observe_only` | `false` | `true` = a machine heavyq only watches: `heavyq fleet` probes it (load, RAM, processes, foreign slots), nothing ever runs there; it takes only `name` and `ssh`/`transport`. See [fleet](fleet.md#configuration) |
| `foreign.name` | required | `[[foreign]]`: another queue `heavyq fleet` observes read-only; a plain name (`[A-Za-z0-9._-]`), not `heavyq`. See [fleet](fleet.md#configuration) |
| `foreign.history` / `foreign.tickets` | one required | its run history (absolute or `~/`) / its live tickets dir (`<prio>-<20-digit ns>-<pid>-<lane>`, `.run` while running) |
| `foreign.format` / `foreign.columns` | `"tsv"` / `["end_epoch","machine","lane","prio","light","wait_s","run_s","exit"]` | the history layout; unknown columns are refused, `end_epoch`, `machine`, `exit` are required |
| `foreign.infra_exits` | `[]` | this queue's exit codes that mean infrastructure, not code (127, 137 and 255 always do) |
| `foreign.machines_dir` | `""` | `<machine>.env` files (`SLOTS`, `LIGHT_SLOTS`, `KIND`; parsed, never sourced): slot counts for the worker cap |
| `foreign.slot_dirs` | `{}` | per configured machine: the directory of this queue's slot lock files there, probed read-only |
| `foreign.aliases` | `{}` | machine names the queue writes, mapped to configured heavyq machine names (`{ "build-1" = "local" }`) |
| `run.need_gh_repos` | `[]` | `[run]`: repository directory names whose commands call the GitHub CLI with this machine's login; runs there behave as if `--need-gh` were given (this machine only). See [remote](remote.md#the-github-cli---need-gh) |
| `pool.name` | required | `[[pool]]`: a named resource handed out with `--needs`; `[a-z][a-z0-9_]{0,31}`. See [pools](pools.md) |
| `pool.values` / `pool.size` | one required | a range `"4100-4199"` or a list of names or integers / a capacity (values `1..size`) |
| `pool.port` | by name | `true` = TCP ports (probed, never in the ephemeral range); default: the pool is called `port` or ends in `_port` |
| `hook.enabled` | `true` | `false` = the Claude Code hook never rewrites |
| `hook.permission` | `""` | `""` = rewritten commands go through Claude Code's permission flow; `"allow"` = approve them |
| `hook.max_wait` | `"5m"` | `--max-wait` added to rewritten commands (and to the Bash tool timeout, up to Claude Code's cap); at least `"1s"`, whole seconds rounded up |
| `hook.binary` | `""` | how rewritten commands call heavyq (default `heavyq` if on PATH, else the absolute path): a command name on PATH or an absolute path, no arguments; a leading `~/` is your home |
| `hook.default_rules` | `true` | apply the built-in rules after yours |
| `hook.rule.match` | required | word prefixes, e.g. `["bun test", "deno task check"]` |
| `hook.rule.priority` / `light` / `lane` / `cache` | `4` / `false` / `""` / `false` | what the rewritten command gets |
| `mentor.holdout_pct` | `20` | share of eligible mentor events logged but not shown, 0-100; `100` = shadow mode. See [mentor](mentor.md) |
| `mentor.auto_mute` | `true` | a rule with too little lift mutes itself |
| `mentor.min_events` / `min_lift_pp` | `200` / `10` | resolved events per arm before the lift is judged / the lift (percentage points) below which the rule is muted |
| `proof.remote` | unset (= `"local"`) | where `item done --run` and `gate run` run proofs: `local`, `auto`, a `[[remote]]` name, or an ordered list `"a,b,c"` (the first with room); `[proof]` in a repo's `heavyq.toml` overrides it. See [items](items.md#where-the-proof-runs) |
| `cache.ttl` | `"24h"` | cached results older than this are ignored and evicted |
| `cache.max_size_mb` / `max_entry_mb` | `512` / `64` | total cap (oldest evicted first) / larger outputs are not stored |
| `cache.cache_failures` | `false` | also replay non-zero exits to later runs (runs that waited always share) |
| `cache.env` | `[]` | variable names whose values are part of the key, e.g. `["CI", "NODE_ENV"]` |

Environment: `HEAVYQ_LANE` (default lane), `HEAVYQ_REMOTE` (default `--remote`), `HEAVYQ_AUTO=0` (never
auto-place), `HEAVYQ_DIR` (queue dir; default `$XDG_RUNTIME_DIR/heavyq`, else `<temp dir>/heavyq-<uid>`, i.e. `$TMPDIR` or `/tmp`),
`HEAVYQ_STEER` (`off|guard|destroy`: overrides the stored stop-gate mode; `off` is the kill switch, see [steer](steer.md)),
`HEAVYQ_SESSION` / `HEAVYQ_ITEM` (default `--session` / `--item`), `HEAVYQ_OUTPUT` / `HEAVYQ_AGENT` (output
mode), `HEAVYQ_HISTORY` (one history file;
default: monthly files in `$XDG_STATE_HOME/heavyq`), `HEAVYQ_CONFIG`, `HEAVYQ_MENTOR=off` (silence the mentor hook), `HEAVYQ_MENTOR_SEED` (fixed experiment seed), `XDG_CACHE_HOME` (result
cache), `CLAUDE_CONFIG_DIR` (`--scope user` target). Commands run locally get `HEAVYQ_MACHINE`, `HEAVYQ_SLOT`,
`HEAVYQ_LANE` and `HEAVYQ_ACTIVE=1` (the hook never rewrites while it is set), plus `HEAVYQ_<POOL>` /
`HEAVYQ_<POOL>_N` for each value leased with `--needs` ([pools](pools.md)).

## Editing the config while tickets wait

There is no daemon: each waiting ticket's own heavyq process decides on every poll whether it may start. It
re-reads the config file when it changes (one `stat` per poll while it does not), validated exactly as at start,
so a slot, a higher `max_load_ratio` or a bigger pool is used by tickets already waiting, within two polls: an
edit is used once two polls in a row read it the same, because an editor that writes the file in place can be
read half-way, and half a config is a valid config nobody wrote.
`heavyq status`, `why`, `top` and the dashboard read the same file, so what they say about a waiting ticket is
the decision it makes (`top` and the dashboard follow edits while they stay open: their probe timeout and the
machines `top --control` may pause too). Turn order does not change: a reload only changes what the tickets can
get, never who goes first.

- **What a waiting ticket keeps.** Its place and priority, its target and candidate machines (`--remote`, auto
  placement: a remote added later is not one of them, with or without `--with` / `--ref`, whose tickets only
  re-check whether this machine still holds the code they asked for), its `--needs`, and the `[cache]` settings
  its cache key and run identity were made with. Admission settings (`[queue]`, `[local]`, the `[[remote]]`
  entries of its candidates, `[[pool]]`) come from the edited file. A ticket moved by `heavyq retarget` keeps the
  machines it moved to in the same way (`auto` then means the remotes of the config at the move; see
  [remote.md](remote.md#moving-a-waiting-ticket-heavyq-retarget)). What placement by tools found (`--tools`) is kept
  too, until an edit changes the `env_path` or transport of one of its remotes: the ticket then asks those remotes
  again, and gives up with exit 2 when none of its machines has the tools any more.
- **A candidate remote removed from the file** is no longer used: status and `why` say `not in this config` for
  it. A ticket that can only run remotely counts it as unreachable from its next poll, so when none of its
  remotes is left (or reachable) it exits 76 after `remote_down_max`, with `<name> (not in the config)
  unreachable` on stderr and in the digest. Putting the remote back before then lets it run.
- **An edit that does not load** (a syntax error, an unsafe or out-of-range value, or the file being gone, even
  the default one: a ticket never drops to the built-in defaults because an editor replaced the file)
  changes nothing: the ticket keeps its last good config and says so once on stderr (not in agent mode):
  `config edit not applied while waiting: ...; keeping the last good config`. Fix the file and the next poll
  picks it up. A ticket with `--needs` also reads the repository's `heavyq.toml` for its pools: while that file
  does not load, the edit is refused the same way (said once) and retried on every poll, so fixing `heavyq.toml`
  alone is enough. Status refuses a file that does not parse or validate outright, so it never explains a ticket
  with a config the ticket refused.
- **Needs that no longer fit.** If the edit removes or shrinks a pool a waiting ticket needs, the ticket gives up
  with exit 2, as it would have at submit.
- **Another config.** A ticket started with another `--config` / `$HEAVYQ_CONFIG` decides with that file. Each
  ticket publishes which config it decides with; where the config status reads has room for it, status says
  `decides under another config than this one` instead of promising a start that the ticket's own config may
  refuse.

## Editor support: JSON Schemas

Four JSON Schemas (draft 7) describe the files heavyq reads and writes. They are generated from the Go types the
parsers use, so a key cannot be in one and not the other; CI fails when `schema/` is stale.

| Schema | Describes | Used for |
|---|---|---|
| [`schema/heavyq.schema.json`](../schema/heavyq.schema.json) | `heavyq.toml` at the repository root: `[[gate]]`, `[[pool]]`, `[anchor]`, `[items]` | completion, hover help and errors for gates (name shape, `prio` 1-9, non-empty `run`) |
| [`schema/config.schema.json`](../schema/config.schema.json) | the user config, `~/.config/heavyq/config.toml` | the same for slots, budgets, `[[remote]]` (required `name`, ssh alias shape; `[local]` does not offer the remote-only keys), `[[hook.rule]]`, `[cache]`, `[mentor]` ranges, durations like `"15m"` |
| [`schema/status.schema.json`](../schema/status.schema.json) | the output of `heavyq status --json` (schema 1) | typed clients; extra fields are allowed, because fields are added within schema 1 |
| [`schema/fleet.schema.json`](../schema/fleet.schema.json) | the output of `heavyq fleet --json` (schema 1) | the same for the fleet report ([fleet](fleet.md#json)) |

The TOML schemas refuse unknown keys, so a typo is underlined as it is typed. heavyq treats the two files
differently: a repository `heavyq.toml` is strict (an unknown key is an error, and CI catches typos), but the
**user config** ignores keys this heavyq does not know and says so once on stderr, so a config written for a newer
heavyq never stops an older one (which may be the one about to upgrade it). `heavyq doctor` lists them. A value
that is wrong, or a key spelled with upper-case letters that would merge with a known one, is still an error.

**Taplo and Even Better TOML (VS Code, Neovim, Helix, Zed).** Put a `#:schema` directive on the first line of the
file; it is a comment to every other TOML reader:

```toml
#:schema https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/heavyq.schema.json
[[gate]]
name = "ready"
run  = ["go test ./..."]
```

and for the user config:

```toml
#:schema https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/config.schema.json
```

To pin the schema to the heavyq you run, replace `main` with the release tag (`v0.4.0`). A checked-out copy works
too, with a relative path: `#:schema ./schema/heavyq.schema.json`.

**Without editing the file.** Associate the schema with the file name in the editor:

```jsonc
// VS Code settings.json (Even Better TOML)
"evenBetterToml.schema.associations": {
  "^(.*/)?heavyq\\.toml$": "https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/heavyq.schema.json",
  "^.*/heavyq/config\\.toml$": "https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/config.schema.json"
}
```

```toml
# .taplo.toml (Taplo CLI and language server)
[[rule]]
include = ["**/heavyq.toml"]
schema.path = "https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/heavyq.schema.json"
```

`taplo lint heavyq.toml` then checks a file in CI. The schemas carry what one key can be on its own (ranges, name
shapes); `heavyq gate status` and `heavyq doctor` are still what validate rules across keys (duplicate gate or
remote names, unknown remotes, pool ranges, the slot total of a machine).

**Regenerating.** `schema/` is written by `tools/schemagen`, a separate Go module (its own `go.mod`), so the main
module keeps its single dependency. After changing a config type, a key or a validation rule:

```bash
cd tools/schemagen && go test ./... && go run .
```

The tests there validate `heavyq.toml` and `examples/config.toml` against the schemas, check that the committed
files are current, that `examples/config.toml` shows every key, and that the user config schema refuses what
heavyq's loader refuses (`schema_validator_parity_test.go`). Rules that only a validator knows (required keys,
ranges, name shapes) are in `tools/schemagen/patches*.go`, built from the patterns and limits `internal/config`
exports; add one there, and a case to the parity test, when you add a rule to a validator.
