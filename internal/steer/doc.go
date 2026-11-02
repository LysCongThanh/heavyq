// Package steer is what heavyq says to an agent without being asked: the
// brief that survives compaction, and the stop gate that wants proof.
//
// The package is pure where it can be. Facts are gathered by the caller
// (internal/cli: items, leases, proofs, machine pressure) and passed in; the
// stop decision (DecideStop), the claim-vs-ledger phrase matcher, and the brief
// renderer are functions of those facts and nothing else, so every case has a
// table test. The only state here is small files under the git common dir: the
// steering mode and one record per agent session.
//
// Design rules (docs/steer.md, the coaching research): facts only, from
// structured fields; fire on state change; baseline-aware (a tree that was red
// before the session started never blocks it); every block names the exact
// next command and an escape; any doubt fails open.
package steer
