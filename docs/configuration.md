# Configuration

`--config FILE`, else `$HEAVYQ_CONFIG`, else `$XDG_CONFIG_HOME/heavyq/config.toml` (`~/.config/heavyq/config.toml`).
Every key is optional. [`examples/config.toml`](../examples/config.toml) has every key with comments.

| Key | Default | Meaning |
|---|---|---|
| `queue.poll` | `"2s"` | how often a waiting ticket retries |
| `queue.force_after` | `"15m"` | head ticket waiting this long bypasses the RAM/load gate |
| `queue.resource_gate` | `true` | `false` = slots only |
| `queue.auto` | `true` | score-based placement for tickets without `--remote` |
| `queue.load_weight_mb` | `500` | `k` in `score = free_mb - k x load1` |
| `queue.hold_for` | `"1m"` | how long a fresh job's RAM budget stays reserved |
| `queue.remote_down_max` | `"10m"` | remote-only ticket exits 76 after its remotes are unreachable this long |
| `queue.probe_timeout` | `"5s"` | timeout of one remote metrics probe |
| `queue.base_ref` | `""` | default `--base-ref` |
| `local.heavy_slots` / `light_slots` | `2` / `1` | slots on this machine |
| `local.legacy_lock_files` | `[]` | use these files as heavy slots (shared with `flock <file>` users) |
| `*.max_load_ratio` | `0.75` | admit while `load1 < ratio x cores` |
| `*.job_mb` / `light_mb` / `reserve_mb` | `3072` / `1024` / `2048` | RAM budget per heavy / light job, and headroom |
| `remote.name`, `remote.ssh` | required | name and ssh alias (`ssh` may be replaced by `transport` + `git_url`) |
| `remote.transport` | `["ssh","-o","BatchMode=yes","-o","ConnectTimeout=10",ssh]` | argv prefix; the remote script is appended as one argument |
| `remote.work_dir` | `".heavyq"` | remote directory (relative to the remote home) |
| `remote.git_url` | `"<ssh>:<work_dir>/git"` | where bare repos are pushed |
| `remote.cache_hook` | `""` | shell run in the worktree before the command; gets `HEAVYQ_CACHE_DIR`, `HEAVYQ_WORKTREE` |
| `remote.env_path` | `""` | directories put first on the remote command's PATH, `:`-separated, `~/` = remote home. See [a spare Mac](remote.md#a-spare-mac-as-a-remote) |
| `pool.name` | required | `[[pool]]`: a named resource handed out with `--needs`; `[a-z][a-z0-9_]{0,31}`. See [pools](pools.md) |
| `pool.values` / `pool.size` | one required | a range `"4100-4199"` or a list of names or integers / a capacity (values `1..size`) |
| `pool.port` | by name | `true` = TCP ports (probed, never in the ephemeral range); default: the pool is called `port` or ends in `_port` |
| `hook.enabled` | `true` | `false` = the Claude Code hook never rewrites |
| `hook.permission` | `""` | `""` = rewritten commands go through Claude Code's permission flow; `"allow"` = approve them |
| `hook.max_wait` | `"5m"` | `--max-wait` added to rewritten commands (and to the Bash tool timeout) |
| `hook.binary` | `""` | how rewritten commands call heavyq (default `heavyq` if on PATH, else the absolute path) |
| `hook.default_rules` | `true` | apply the built-in rules after yours |
| `hook.rule.match` | required | word prefixes, e.g. `["bun test", "deno task check"]` |
| `hook.rule.priority` / `light` / `lane` / `cache` | `4` / `false` / `""` / `false` | what the rewritten command gets |
| `mentor.holdout_pct` | `20` | share of eligible mentor events logged but not shown, 0-100; `100` = shadow mode. See [mentor](mentor.md) |
| `mentor.auto_mute` | `true` | a rule with too little lift mutes itself |
| `mentor.min_events` / `min_lift_pp` | `200` / `10` | resolved events per arm before the lift is judged / the lift (percentage points) below which the rule is muted |
| `cache.ttl` | `"24h"` | cached results older than this are ignored and evicted |
| `cache.max_size_mb` / `max_entry_mb` | `512` / `64` | total cap (oldest evicted first) / larger outputs are not stored |
| `cache.cache_failures` | `false` | also replay non-zero exits to later runs (runs that waited always share) |
| `cache.env` | `[]` | variable names whose values are part of the key, e.g. `["CI", "NODE_ENV"]` |

Environment: `HEAVYQ_LANE` (default lane), `HEAVYQ_REMOTE` (default `--remote`), `HEAVYQ_AUTO=0` (never
auto-place), `HEAVYQ_DIR` (queue dir; default `$XDG_RUNTIME_DIR/heavyq`, else `/tmp/heavyq-<uid>`),
`HEAVYQ_SESSION` / `HEAVYQ_ITEM` (default `--session` / `--item`), `HEAVYQ_OUTPUT` / `HEAVYQ_AGENT` (output
mode), `HEAVYQ_HISTORY` (one history file;
default: monthly files in `$XDG_STATE_HOME/heavyq`), `HEAVYQ_CONFIG`, `HEAVYQ_MENTOR=off` (silence the mentor hook), `HEAVYQ_MENTOR_SEED` (fixed experiment seed), `XDG_CACHE_HOME` (result
cache), `CLAUDE_CONFIG_DIR` (`--scope user` target). Commands run locally get `HEAVYQ_MACHINE`, `HEAVYQ_SLOT`,
`HEAVYQ_LANE` and `HEAVYQ_ACTIVE=1` (the hook never rewrites while it is set), plus `HEAVYQ_<POOL>` /
`HEAVYQ_<POOL>_N` for each value leased with `--needs` ([pools](pools.md)).
