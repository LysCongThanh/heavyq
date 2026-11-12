**A remote run notices a silently dropped link in about a minute, not two hours.** The default transport now adds
`-o ServerAliveInterval=15 -o ServerAliveCountMax=4`. Before, when a remote lost power or a VPN or NAT mapping expired
mid-run, nothing reached ssh, so heavyq waited for TCP keepalive (about two hours) holding the ticket and the agent's
tool call, and the remote kept the command and its slot (the next run there exited 77). Now ssh exits 255 and the
remote stops the command. A custom `transport` that runs ssh should carry the same options (docs/remote.md).
