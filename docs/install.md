# Install

heavyq is one static binary for Linux and macOS (amd64 and arm64). Pick one way.

## The install script

```bash
curl -fsSL https://github.com/LysCongThanh/heavyq/releases/latest/download/install.sh | sh
```

It finds your OS and CPU, downloads the release archive and `checksums.txt`, **checks the archive's SHA-256
against `checksums.txt`** (a mismatch installs nothing), and puts `heavyq` in `~/.local/bin`. It needs `curl`,
`tar` and `sha256sum` or `shasum`, and it does not ask for root.

```bash
# a given version, another prefix (installs into /usr/local/bin), and a check of the cosign signature
curl -fsSL https://github.com/LysCongThanh/heavyq/releases/latest/download/install.sh \
  | sh -s -- --version 0.4.0 --prefix /usr/local --verify
```

| Option | Meaning |
|---|---|
| `--version X.Y.Z` | install this release (default: the latest) |
| `--prefix DIR` | install into `DIR/bin` (default `~/.local`; use `sudo` yourself for `/usr/local`) |
| `--verify` | also verify the [cosign signature](#verify-a-release) of `checksums.txt`; needs `cosign`, refuses releases that have no signature |

If you would rather read it first: `curl -fsSLO <url>`, read `install.sh`, `sh install.sh`. The script is attached to
every release, so the URL of a version never changes (releases are immutable), and it is listed in that release's
signed `checksums.txt`. It is tested in CI on Linux and macOS against a local fake release (matching and tampered
archives, a duplicated checksum line, `--verify`) and against real releases.

## Go

```bash
go install github.com/LysCongThanh/heavyq@latest
```

## A download by hand

Archives are on [Releases](https://github.com/LysCongThanh/heavyq/releases): `heavyq_<version>_<os>_<arch>.tar.gz`
with `os` = `linux` or `darwin`, `arch` = `amd64` or `arm64`. An archive holds the binary, `LICENSE`, `README.md`,
`examples/` and the man page (`man/man1/heavyq.1`).

```bash
tar -xzf heavyq_0.4.0_linux_amd64.tar.gz heavyq
install -m 0755 heavyq ~/.local/bin/heavyq
```

## Verify a release

Every release carries, next to the archives: `checksums.txt` (SHA-256), a cosign signature for each archive and for
`checksums.txt` (`<file>.sigstore.json`), an SPDX SBOM per archive, and a GitHub build-provenance attestation.
The signatures and the attestation are made by this repository's release workflow through Sigstore (keyless: no
signing key exists to leak), so they say "this file was built by `release.yml` at tag `vX.Y.Z` of
`LysCongThanh/heavyq`".

**cosign** 3 or later; cosign 2 fails on these bundles with "bundle does not contain cert for verification" (one `.sigstore.json` bundle in the Sigstore bundle format holds the signature, the certificate and the transparency-log entry):

```bash
V=0.5.0
cosign verify-blob \
  --bundle heavyq_${V}_linux_amd64.tar.gz.sigstore.json \
  --certificate-identity "https://github.com/LysCongThanh/heavyq/.github/workflows/release.yml@refs/tags/v${V}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  heavyq_${V}_linux_amd64.tar.gz
```

To check the lot, verify `checksums.txt` the same way (its bundle is `checksums.txt.sigstore.json`), then compare
the archive: `grep " heavyq_${V}_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -` (macOS: `shasum -a 256 -c -`).
The install script does exactly this with `--verify`.

**GitHub attestation** (needs `gh` 2.49 or later):

```bash
gh attestation verify heavyq_${V}_linux_amd64.tar.gz --repo LysCongThanh/heavyq
```

With an older `gh`, the attestation is still public: `gh api repos/LysCongThanh/heavyq/attestations/sha256:<digest>`
where `<digest>` is the archive's SHA-256.

Releases v0.1.0 to v0.3.0 predate the cosign signatures; they have checksums, SBOMs and the attestation only. A
published release is immutable: its assets cannot be replaced or deleted and its tag cannot be moved.

## Shell completions

Generated from the command table, so they list every command, subcommand and flag of the version you run:

```bash
# bash (3.2 on macOS and later; no bash-completion package needed)
source <(heavyq completion bash)                     # in ~/.bashrc
# or once:  heavyq completion bash > ~/.local/share/bash-completion/completions/heavyq

# zsh: put it on $fpath before compinit
heavyq completion zsh > "${fpath[1]}/_heavyq"

# fish
heavyq completion fish > ~/.config/fish/completions/heavyq.fish
```

Completion knows the first word (commands), subcommands (`gate status|run`, `hook install claude`), flags and their
values (`--remote local|auto`, `--scope local|project|user`, paths for `--repo` and `--config`). After `--` it
completes the program you are queuing.

## Man page and help

`man heavyq` after installing the page from the archive:

```bash
install -Dm644 man/man1/heavyq.1 ~/.local/share/man/man1/heavyq.1     # then: man heavyq
```

`heavyq --help` is the overview. Every command has its own usage block, with its flags and a link to its docs:
`heavyq status --help`, `heavyq gate --help`, `heavyq 4 --help` (the run form), or `heavyq help <command>`. The
man page, the completions and these blocks are generated from one table of commands in `internal/cli`, and tests
fail when it and the code disagree.

## Uninstall

`rm ~/.local/bin/heavyq` (or wherever you installed it). heavyq keeps its queue in `$XDG_RUNTIME_DIR/heavyq`
(a temp directory; without a runtime directory, `heavyq-<uid>` in the per-user temp directory), its ledger in `$XDG_STATE_HOME/heavyq` and its result cache in `$XDG_CACHE_HOME/heavyq`;
delete them if you want them gone.
