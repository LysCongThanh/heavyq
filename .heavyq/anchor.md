# Goal: v0.6 — trustworthy numbers, collision-free claims

Dogfood: agents claim these with `heavyq item claim <id>` and prove them through the queue on a remote.
A proof is a named gate in heavyq.toml (same name as the item): a test that must exist and pass (`go test -run` that
matches nothing would pass, so the gate greps for the test's PASS line). Prove with
`heavyq item done <id> --run --remote linuxi`, or `heavyq gate run <id> --remote linuxi`; no `sh -c` to type.

- [ ] proof-scope: a gate can declare the paths its proof depends on; a docs-only commit keeps the proof (#141)  `proof: gate:proof-scope`
- [ ] recap-accuracy: done+stale labelled as such; throughput counts every proof, not only claim→proof (#141)  `proof: gate:recap-accuracy`
- [ ] next-claim: `item claim --next` respects `after:`, refuses path overlap with live leases, WIP cap (#97)  `proof: gate:next-claim`
- [ ] tick-free-done: `done` from a parallel worktree records the event without editing the anchor (#119)  `proof: gate:tick-free-done`
- [ ] stance-hygiene: stance dirs ignore non-stance files; narrow built-in keywords; holdout arm (#51, #92)  `proof: gate:stance-hygiene`
