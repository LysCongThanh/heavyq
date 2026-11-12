# Pools: ports, databases and browsers for parallel agents

Parallel agents collide on ports and databases more often than on CPU. A **pool** is a named set of values
(`port`: 4100-4199, `pg`: three test databases, `browser`: two seats). A run asks for some with `--needs`,
holds them while it runs, and gives them back when it ends, the way it holds a slot.

```toml
# ~/.config/heavyq/config.toml, or heavyq.toml at the repository root
[[pool]]
name = "port"
values = "4100-4199"                      # a range ...

[[pool]]
name = "pg"
values = ["app_wt1", "app_wt2", "app_wt3"]  # ... or a list of names (strings or integers)

[[pool]]
name = "browser"
size = 2                                  # capacity only: the values are 1 and 2
```

```bash
heavyq 4 --needs port:2,pg,browser -- npm run e2e
```

The command gets one environment variable per value: `HEAVYQ_PORT_1`, `HEAVYQ_PORT_2`, `HEAVYQ_PG`,
`HEAVYQ_BROWSER`. One value is `HEAVYQ_<POOL>`, several are `HEAVYQ_<POOL>_1` .. `_N` (the pool name upper-cased).

## Rules

- **All or nothing.** A ticket is admitted only when its slot and every need can be taken in the same scheduling
  round, under the queue lock. Otherwise it holds nothing, so two tickets can never each hold half of what the other
  wants: no deadlock.
- **Released with the run.** Each value is a `flock(2)` file under `<queue dir>/pools/<pool>/<value>.lock`, held by the
  heavyq process of the run, with the same liveness as a slot: if heavyq dies (even by `SIGKILL`), the kernel frees the
  value and the dead ticket is pruned. The command never inherits the lock descriptors.
- **Ports are probed.** A pool is a port pool when it is called `port` or ends in `_port` (or has `port = true`;
  `port = false` turns it off). heavyq never hands out a port that something listens on right now (a listen probe on
  `127.0.0.1` and `::1`), and never one inside the OS ephemeral range, where any outgoing connection may take it:
  `/proc/sys/net/ipv4/ip_local_port_range` on Linux, `sysctl net.inet.ip.portrange.first/last` on macOS, else
  49152-65535. Pick ranges outside it (4100-4199 is fine).
- **Refused early.** An unknown pool, `port:200` from a pool with 100 usable ports, or a count above 64 exits 2 before
  queueing: waiting for it would never end.
- **Fair to the rest.** Order stays strict, except that a ticket waiting for a pool does not hold back a ticket behind
  it that asks for none of the same pools and could start now.
- **Local only.** Pool values are resources of this machine. A ticket with needs runs locally: `--remote auto` (and the
  default placement) means local, a named `--remote` is an error, and a gate with `needs` must not name a remote.

## Where pools are defined

`[[pool]]` tables go in the user config and in `heavyq.toml`. For a run, the user's pools apply, and a pool of the same
name in the repository's `heavyq.toml` replaces the user's (the repository knows its own databases). A pool name is
`[a-z][a-z0-9_]{0,31}`; values are `[A-Za-z0-9][A-Za-z0-9._-]{0,63}` (they become file names and environment values).
`values` and `size` are exclusive; a range is `"a-b"` with `a <= b`. Unknown keys, duplicate pools and names that would
collide with a variable heavyq sets or reads (`lane`, `slot`, `machine`, `history`, `flow`, `test`, ...) are errors: the
command, and any heavyq it runs, would otherwise see the pool's value in that variable.
Two pools in different repositories that share a name and a value share the lock, which is what you want for one
physical database.

## Gates

A gate may declare `needs`; each of its commands holds them while it runs:

```toml
[[gate]]
name = "e2e"
run = ["npm run e2e"]
needs = ["pg", "port:2"]
```

`heavyq gate run e2e` passes `--needs pg,port:2` to every command. The ledger run id does not include the values, so
the cache and proof treat runs on different databases as the same run.

## What you see

- `heavyq status`: a `POOLS` section, `pg 2/3 held: a (app_wt1) · b (app_wt2)`. `status --json` has `pools`
  (`name`, `capacity`, `held`, `holders[]` with `value`, `lane`, `ticket`) and each ticket carries `needs` and, once
  running, `pools`. Status shows the pools of the user config plus those of the repository in the current directory.
- `heavyq why`: `waiting for pg: 3/3 held by a, b, c`, or `waiting for port (needs 2): 3/4 held by a (1 in use outside
  the queue)`. The anti-starvation bypass skips the load check only, never RAM or a pool, so it is not promised for such a wait.
- The Tower's `starved` card names the pool a long-waiting head waits for. The dashboard's Machines section shows
  one chip per pool (`pg 2/3`, holders in the tooltip).
- The ledger (`heavyq history`, [ledger](ledger.md)) records `needs` (`port:2,pg`) and `pools` (`port=4100,4101
  pg=app_wt1`) on each run, as flat string fields.

## Limits

Remote machines have no pools: a remote cannot satisfy `--needs` yet. (`--needs` / `needs` name pool values, leased on
this machine; `--tools` / `tools` name commands that must resolve on a remote, see
[remote](remote.md#placed-where-its-tools-are---tools).) Values are leased, not created: heavyq does not start
databases or free ports, it only makes sure two runs never get the same name or port from the same pool.
