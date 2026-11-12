**A remote run notices a silently dropped link in about a minute, not two hours.** The default transport now adds
`-o ServerAliveInterval=15 -o ServerAliveCountMax=4`. Before, when a VPN or NAT mapping expired or a remote lost power
mid-run, nothing reached ssh, so heavyq waited for TCP keepalive (about two hours) holding the ticket and the agent's
tool call. Now ssh exits 255 about a minute later and the ticket is freed. This frees only the local side: a remote
that stays up keeps the command and its slot until its own sshd notices the dead connection, so set
`ClientAliveInterval 15` and `ClientAliveCountMax 4` in that machine's `sshd_config`. A custom `transport` that runs
ssh should carry the same `ServerAlive*` options (docs/remote.md).
