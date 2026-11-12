**`heavyq fleet`: observe every machine and every queue.** A read-only, daemonless report of the whole fleet,
other queues included. Per machine: cores, load/cores, free RAM, memory pressure, an OVERLOAD flag with its
rule, the busiest processes attributed to a queue and lane (or "outside"), busy slots by owner and a suggested
worker cap (cores / heavy slots of every queue, reasoning shown). Per queue and machine over `--since`: runs,
run-hours, wait p50/p90/p95, run p50, throughput and red runs split infra vs code; live running and waiting
tickets with lane names. Other queues are `[[foreign]]` tables (a TSV history read from its end, a tickets dir,
machine files, slot dirs); machines heavyq only watches are `[[remote]] observe_only = true`. Output as text,
`--json` (`schema/fleet.schema.json`), `--md FILE` written atomically for cron and other agents, or a digest
under 4 KB in agent mode. Machines are probed in parallel, each within `--timeout`. See [docs/fleet.md](docs/fleet.md).
