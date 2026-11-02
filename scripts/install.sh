#!/bin/sh
# heavyq installer: downloads a release archive, checks its sha256 against the release's checksums.txt
# and installs the binary. Attached to every release, so the URL of a version never changes:
#   curl -fsSL https://github.com/LysCongThanh/heavyq/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/LysCongThanh/heavyq/releases/latest/download/install.sh | sh -s -- --prefix /usr/local
# Prefer download, read, run if you do not want to pipe into a shell.
#
# Options:
#   --version X.Y.Z   install this release (default: the latest)
#   --prefix DIR      install into DIR/bin (default: $HOME/.local, so ~/.local/bin)
#   --verify          also verify the cosign signature of checksums.txt (needs cosign on PATH)
#   -h, --help        show this text
# Environment (for tests and mirrors): HEAVYQ_RELEASE_BASE is the directory URL that holds the
# archive and checksums.txt; it needs --version.
set -eu

REPO="LysCongThanh/heavyq"
# Who may sign a release: this repository's release workflow on a v* tag.
SIGNER_RE="^https://github.com/${REPO}/\\.github/workflows/release\\.yml@refs/tags/v"
OIDC_ISSUER="https://token.actions.githubusercontent.com"

version=""
prefix=""
verify=0

say() { printf 'heavyq-install: %s\n' "$*"; }
die() { printf 'heavyq-install: error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
usage: install.sh [--version X.Y.Z] [--prefix DIR] [--verify]
  --version X.Y.Z   install this release (default: the latest)
  --prefix DIR      install into DIR/bin (default: $HOME/.local, so ~/.local/bin)
  --verify          also verify the cosign signature of checksums.txt (needs cosign on PATH)
  -h, --help        show this text
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || die "--version needs a value"; version=$2; shift 2 ;;
    --version=*) version=${1#*=}; shift ;;
    --prefix) [ $# -ge 2 ] || die "--prefix needs a value"; prefix=$2; shift 2 ;;
    --prefix=*) prefix=${1#*=}; shift ;;
    --verify) verify=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

if [ -z "$prefix" ] && [ -n "${HOME:-}" ]; then prefix="$HOME/.local"; fi
[ -n "$prefix" ] || die "no install prefix: set --prefix or HOME"
version=${version#v}
case "$version" in
  "" | [0-9]*.[0-9]*.[0-9]*) ;;
  *) die "bad version '$version' (want X.Y.Z)" ;;
esac
case "$version" in *[!0-9A-Za-z.+-]*) die "bad version '$version'" ;; esac

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl
need tar
need uname

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS $(uname -s): heavyq ships for Linux and macOS" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m): heavyq ships for amd64 and arm64" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
  die "sha256sum or shasum is required"
fi

if [ -n "${HEAVYQ_RELEASE_BASE:-}" ]; then
  [ -n "$version" ] || die "HEAVYQ_RELEASE_BASE needs --version"
  base=${HEAVYQ_RELEASE_BASE%/}
else
  if [ -z "$version" ]; then
    # /releases/latest redirects to /releases/tag/vX.Y.Z
    final=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") \
      || die "cannot look up the latest release"
    version=${final##*/v}
    case "$version" in
      [0-9]*.[0-9]*.[0-9]*) ;;
      *) die "cannot read the latest version from $final" ;;
    esac
  fi
  base="https://github.com/${REPO}/releases/download/v${version}"
fi

archive="heavyq_${version}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/heavyq-install.XXXXXX") || die "cannot create a temp directory"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "heavyq ${version} for ${os}/${arch}"
curl -fsSL --retry 3 -o "$tmp/$archive" "$base/$archive" || die "download failed: $base/$archive"
curl -fsSL --retry 3 -o "$tmp/checksums.txt" "$base/checksums.txt" || die "download failed: $base/checksums.txt"

if [ "$verify" = 1 ]; then
  need cosign
  curl -fsSL --retry 3 -o "$tmp/checksums.txt.sigstore.json" "$base/checksums.txt.sigstore.json" \
    || die "no signature for this release (releases before the first signed one have none)"
  cosign verify-blob --bundle "$tmp/checksums.txt.sigstore.json" \
    --certificate-identity-regexp "$SIGNER_RE" --certificate-oidc-issuer "$OIDC_ISSUER" \
    "$tmp/checksums.txt" >/dev/null 2>&1 || {
    # cosign 2 cannot read the bundle format the release workflow writes ("bundle does not contain cert").
    cv=$(cosign version 2>/dev/null | sed -n 's/^GitVersion:[[:space:]]*v\{0,1\}//p' | head -n 1)
    case "$cv" in
      [012].*) die "cosign ${cv} cannot read these signature bundles; verifying needs cosign 3 or later" ;;
    esac
    die "cosign signature of checksums.txt is not valid"
  }
  say "cosign signature of checksums.txt verified"
fi

# Exactly one line must name the archive: "<sha256>  <file>".
want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not listed in checksums.txt"
[ "$(printf '%s\n' "$want" | wc -l | tr -d ' ')" = 1 ] || die "$archive is listed more than once in checksums.txt"
got=$(sha256_of "$tmp/$archive")
[ "$got" = "$want" ] || die "sha256 mismatch for $archive (expected $want, got $got); nothing installed"
say "sha256 verified"

mkdir "$tmp/x"
tar -xzf "$tmp/$archive" -C "$tmp/x" heavyq || die "$archive has no heavyq binary"
[ -f "$tmp/x/heavyq" ] || die "$archive has no heavyq binary"

bindir="$prefix/bin"
mkdir -p "$bindir" || die "cannot create $bindir"
# Copy next to the target, then rename: a running heavyq is replaced atomically, never half-written.
cp "$tmp/x/heavyq" "$bindir/.heavyq.new.$$" || die "cannot write to $bindir"
chmod 0755 "$bindir/.heavyq.new.$$"
mv -f "$bindir/.heavyq.new.$$" "$bindir/heavyq" || die "cannot install $bindir/heavyq"
say "installed $bindir/heavyq"

case ":${PATH:-}:" in
  *":$bindir:"*) ;;
  *) say "note: $bindir is not on your PATH" ;;
esac
