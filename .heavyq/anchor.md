# Goal: v0.5 — long runs that report themselves (visible, self-checking)

Dogfood: agents claim these with `heavyq item claim <id>` and prove them through the queue on a remote.
A proof is a named gate in heavyq.toml (same name as the item): a test that must exist and pass (`go test -run` that
matches nothing would pass, so the gate greps for the test's PASS line). Prove with
`heavyq item done <id> --run --remote linuxi`, or `heavyq gate run <id> --remote linuxi`; no `sh -c` to type.

- [ ] flow-collectors: git/gh collectors, EWMA baseline, flowview on flowstate types (#113)  `proof: gate:flow-collectors`
- [ ] cli-race-fast: internal/cli under -race in ≤ 5 min (#114)  `proof: gate:cli-race-fast`
- [ ] permission-matcher: preflight matcher checked against real decisions; uncertain only warns (#108)  `proof: gate:permission-matcher`
- [ ] size-gate: claims refuse items an agent cannot finish and prove (#96)  `proof: gate:size-gate`
- [ ] proof-remote: proofs and gates run where heavy work is configured to go (#115)  `proof: gate:proof-remote`
