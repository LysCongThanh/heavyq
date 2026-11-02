# Goal: v0.5 — long runs that report themselves (visible, self-checking)

Dogfood: agents claim these with `heavyq item claim <id>` and prove them through the queue on a remote.
A proof is a test that must exist and pass (`go test -run` that matches nothing would pass, so we grep).

- [ ] flow-collectors: git/gh collectors, EWMA baseline, flowview on flowstate types (#113)  `proof: go test -count=1 -run '^TestReplaySerialCIConvoy$' -v ./internal/flow/... | grep -q -- '--- PASS: TestReplaySerialCIConvoy'`
- [ ] cli-race-fast: internal/cli under -race in ≤ 5 min (#114)  `proof: timeout 300 go test -race -count=1 ./internal/cli/...`
- [ ] permission-matcher: preflight matcher checked against real decisions; uncertain only warns (#108)  `proof: go test -count=1 -run '^TestPermissionMatcherCorpus$' -v ./internal/preflight | grep -q -- '--- PASS: TestPermissionMatcherCorpus'`
- [ ] size-gate: claims refuse items an agent cannot finish and prove (#96)  `proof: go test -count=1 -run '^TestE2EClaimRefusesOversizedItem$' -v ./internal/cli | grep -q -- '--- PASS: TestE2EClaimRefusesOversizedItem'`
- [ ] proof-remote: proofs and gates run where heavy work is configured to go (#115)  `proof: go test -count=1 -run '^TestE2EItemDoneRunUsesProofRemote$' -v ./internal/cli | grep -q -- '--- PASS: TestE2EItemDoneRunUsesProofRemote'`
