- **Hook: `hook.permission = "allow"` approves no more than a Claude Code prefix rule would.** It approved the whole
  command, including any leading assignment (`GOFLAGS=-toolexec=...`, `LD_PRELOAD=...`) and any `cd DIR &&`. Now
  a command with an assignment Claude Code does not strip for prefix rules, or a `cd` that may leave the agent's
  cwd (including a glob like `.[.]`, which bash before 5.2 expands to `..`), is still rewritten but gets no
  decision, so Claude Code asks as usual.
- **Hook: `hook.log` and the agent note are redacted.** The rewritten command was logged as typed, so a
  `DATABASE_PASSWORD=...` assignment or an `-api-key=...` flag ended up in clear in a long-lived log, and the
  note added to the agent's context echoed the command unredacted and unbounded. Both now go through the
  same redaction as the ledger, and the note is capped at 200 characters of command.
