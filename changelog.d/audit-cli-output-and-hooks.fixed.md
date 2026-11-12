- **Agent mode keeps its line budget.** The `sh -c` pipe warning is a line of the digest for an agent, the
  "identical run in progress" notice and remote setup notes are left to the digest, a remote job no longer prints
  its banner into an agent's output, and a remote setup error is one bounded line. A command line with a newline,
  carriage return or escape byte (a multi-line `sh -c` script) no longer splits or colours the running line, the
  digest header or `why`.
- **The mentor hook never records a note it did not show.** Its decision runs under the 15 ms budget and is written
  only when it came back in time; a late one left an "inject" event the agent never saw (biasing the measured lift
  and auto-mute) and marked the run as looked at, so the note was never said.
- **Preflight predicts the rewrite the hook really writes.** When PATH does not find this binary, the hook writes
  its absolute path, which a rule for `heavyq ...` does not match; the permission check judged `heavyq ...` and
  said such calls would not prompt.
