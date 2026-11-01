# Dashboard, JSON snapshot and control API

`heavyq dash [--control] [--addr 127.0.0.1:0] [--interval 2s] [--open]` serves one embedded page and the
API below. It prints a link of the form `http://127.0.0.1:PORT/#token=TOKEN`. The token is generated at
every start, lives only in the URL fragment (never sent in requests as a URL), and must be sent as
`Authorization: Bearer TOKEN` on every API call.

## Security model

- Listens on loopback only (`127.0.0.1`, `::1`, `localhost`); other addresses are refused.
- Requests whose `Host` header is not the loopback address and port are refused (DNS rebinding).
- Requests carrying an `Origin` other than the dashboard's own are refused (cross-site requests).
- The page is served with `default-src 'none'` and `connect-src 'self'`: it loads nothing from the network.
- Read-only by default. State-changing endpoints answer 403 unless the server runs with `--control`.
- Command lines are passed through a redactor before they are served (also in `status --json`).

Anyone who can run processes as your user can already do everything the dashboard does, so the goal is to
keep other users, other machines and web pages out, not to separate privileges within your account.

## Endpoints (`/api/v1/`)

| Method & path | Answer |
|---|---|
| `GET meta` | `{"version","host","schema":1,"control":bool,"interval_ms"}` |
| `GET snapshot` | the current snapshot (same as `heavyq status --json`) |
| `GET stream` | Server-Sent Events: `event: snapshot` with the snapshot every `interval_ms`; `: keep-alive` comments every 15 s. One producer serves every open stream. |
| `GET history?n=100` | `{"records":[...]}`, newest last, at most 1000 |
| `GET insights?window=1h` | summary of history over `1h`, `6h`, `24h` or `7d`, in 12 buckets |
| `POST tickets/{id}/cancel` | 202. Waiting: leaves the queue. Running: SIGTERM to the process tree, SIGKILL after 10 s. Exit 78. |
| `POST tickets/{id}/prio` `{"prio":1-9}` | 202 for waiting tickets, 409 for running ones |
| `POST machines/{name}/pause`, `.../resume` | 200. Paused machines admit nothing new. |

Errors are `{"error":"..."}` with 400, 401, 403, 404, 409 or 421 (wrong `Host`).

## Snapshot (schema 1)

The Go definition is the reference: [`internal/snapshot/snapshot_types.go`](../internal/snapshot/snapshot_types.go).
Fields may be added within schema 1; renaming or removing one bumps `schema`.

- `machines[]`: `free_mb`, `total_mb`, `reserved_mb` (RAM promised to jobs that started less than
  `hold_for` ago), `load1`, `max_load`, the job budgets, `slots[]` (`free`, `held` by a ticket, or `outside`:
  locked by something that isn't in the queue), `paused`, and `admits` (`""` when a heavy job could start now,
  otherwise the reason).
- `running[]`: lane, priority, command, machine/slot, `wait_s`, `run_s`, `forced` (admitted by the
  anti-starvation bypass), and `cpu_pct` / `rss_mb` of the whole process tree for local jobs.
- `waiting[]`: in turn order, with `reasons` per candidate machine (`""` = starts on its next poll) and
  `bypass_in_s` (head of line only, `-1` otherwise).
- `coalesced[]`: identical `--cache` runs waiting for one leader.
- `cpu`: local CPU and memory per lane, and outside the queue per executable (top 8 plus a total).
- `story[]`: the state of the queue as sentences, built in Go so the dashboard, `heavyq status` and
  `heavyq why` say the same thing. Each sentence is `{"text": "...", "parts": [{"text", "em", "num", "lane",
  "hot"}]}`: `text` is plain, `parts` add emphasis (`em`; `num` for a figure; `lane` for a lane name; `hot`
  for a problem). `em`, `num`, `lane` and `hot` are omitted when false or empty, and the `text` of all parts
  joined is the sentence's `text`. Countdowns ("skips the RAM and load checks in 2m 10s") are as of
  `generated_at`. The page renders `story` when present and falls back to its own copy of the logic for older
  servers.

## Control without the dashboard

Requests are files in `<queue dir>/ctl/`: `<ticket>.cancel`, `<ticket>.prio`, `pause-<machine>`. They are
what `heavyq cancel|reprio|pause|resume` and the dashboard write, and what each job's heavyq process reads
about once per poll interval. Scripts may write them directly. Ticket names are the `id` fields of the
snapshot.
