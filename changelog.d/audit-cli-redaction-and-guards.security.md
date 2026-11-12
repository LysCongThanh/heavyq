- **`hook.log` redacts the rewritten command.** A secret in an env assignment (`GITHUB_TOKEN=... go test`) was kept
  verbatim in the log the docs tell users to quote.
- **Blocked items' needs and the anchor goal are redacted everywhere they are shown** (`why`, `status --json`, the
  dashboard, `top`, `brief`, the session context, `item ls`), as they already were in notifications.
- **`heavyq trust` stays refused under a coding agent whatever the output mode.** `HEAVYQ_OUTPUT=human` or
  `HEAVYQ_AGENT=0` (an output preference, often exported for every shell) turned the guard off.
- **The dashboard closes refused and idle connections.** A local user without the token could hold sockets and
  goroutines forever with a request body it never sent, or an idle keep-alive connection.
