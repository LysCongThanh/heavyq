**Item reasons no longer show the proof command unredacted.** `item ls --json` redacted `proof.value` but its
`reason` ("not run on this code yet: ...", "failed on this code: ...", "flaky on this code: ...") carried the raw
command, and `item done` printed the same text when it refused. A secret in a proof command (`--token=...`,
`API_KEY=...`) now goes through the redactor there too, so agent transcripts and logs never hold it.
