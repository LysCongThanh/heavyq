# Security policy

## Supported versions

Only the latest release gets fixes. heavyq is pre-1.0, so a fix ships as a new patch or minor release
rather than a backport.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through GitHub:
[Security > Report a vulnerability](https://github.com/LysCongThanh/heavyq/security/advisories/new).

Useful reports include the heavyq version (`heavyq --version`), OS, the config (host names removed), and
the commands that reproduce it. You should get a first reply within 7 days. Once a fix is released, the
advisory is published with credit, unless you prefer to stay anonymous.

## What counts

heavyq runs other people's commands, locally and over SSH, so these matter most:

- Making heavyq run something unintended on a remote: injection through a repo name, lane, ref, path or
  config value; escaping the per-commit work tree.
- The Claude Code hook rewriting a command into something with a different meaning than the original.
- The dashboard (`heavyq dash`): reaching it from another origin or host, bypassing the token, `Host` or
  `Origin` checks, or using control actions when it was started without `--control`.
- Secrets from command lines leaking into `status --json`, the dashboard or history despite redaction.
- Another local user tampering with your queue directory (for example a pre-created `/tmp/heavyq-<uid>`).

Out of scope: anything that already requires running arbitrary commands as the same user.

## Verifying releases

Release archives carry GitHub build provenance (Sigstore-signed, built by `.github/workflows/release.yml`):

```bash
gh attestation verify heavyq_<version>_linux_amd64.tar.gz --repo LysCongThanh/heavyq
```
