**Command-line redaction hides more credential forms.** Before this fix, `status --json`, `why`, the dashboard,
digests, recap and the ledger could show part or all of a secret. Now hidden: a quoted or multi-word value
(`--password "a b c"`, `PASSWORD='a b'`, and an argv element with spaces, whose quotes the shell already
removed), `curl -u`/`--user user:password`, any `Authorization:` header value with or without a scheme (a
quoted header up to its closing quote), mysql `-pPASSWORD` (whole, even with `&`, `;` or `|` in it or in an earlier argument such as `-e` SQL), `sshpass -p`, `docker login -p`,
`set <secret-looking key> <value>` (aws/npm config) and a URL password that contains a raw `@`. Look-alikes such as `go test -p 4`, `ssh -p 22`, `mysql -p` (password prompt)
and `docker run -u 1000:1000` still read as before.
