#!/bin/sh
# Tests scripts/install.sh against a local fake release (file:// URLs), so it needs no network, and
# optionally against a real release (HEAVYQ_TEST_NETWORK=1). Runs on Linux and macOS (CI does both).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
installer="$here/install.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/heavyq-install-test.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "unsupported OS" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; arm64 | aarch64) arch=arm64 ;; *) echo "unsupported CPU" >&2; exit 1 ;; esac

if command -v sha256sum >/dev/null 2>&1; then sum() { sha256sum "$1" | cut -d ' ' -f 1; }; else sum() { shasum -a 256 "$1" | cut -d ' ' -f 1; }; fi

fails=0
ok() { printf 'ok   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; fails=$((fails + 1)); }

# check NAME EXPECTED_STATUS(0|fail) CMD... : runs CMD, compares success/failure.
check() {
  name=$1; want=$2; shift 2
  if "$@" >"$work/out" 2>&1; then got=0; else got=fail; fi
  if [ "$got" = "$want" ]; then ok "$name"; else bad "$name (wanted $want, got $got)"; sed 's/^/     /' "$work/out"; fi
}

# A release directory with a fake "heavyq" that prints its version.
mkrelease() { # mkrelease DIR VERSION
  dir=$1; ver=$2
  mkdir -p "$dir" "$work/stage"
  printf '#!/bin/sh\necho "heavyq %s"\n' "$ver" >"$work/stage/heavyq"
  chmod 0755 "$work/stage/heavyq"
  tar -czf "$dir/heavyq_${ver}_${os}_${arch}.tar.gz" -C "$work/stage" heavyq
  printf '%s  heavyq_%s_%s_%s.tar.gz\n%s  heavyq_%s_other_arch.tar.gz\n' \
    "$(sum "$dir/heavyq_${ver}_${os}_${arch}.tar.gz")" "$ver" "$os" "$arch" \
    "0000000000000000000000000000000000000000000000000000000000000000" "$ver" >"$dir/checksums.txt"
}

install_from() { # install_from DIR PREFIX [extra args]
  dir=$1; pfx=$2; shift 2
  HEAVYQ_RELEASE_BASE="file://$dir" sh "$installer" --version 9.9.9 --prefix "$pfx" "$@"
}

good="$work/rel-good"
mkrelease "$good" 9.9.9

# 1. happy path: installed, executable, runs, no temp files left beside it.
pfx="$work/p1"
check "installs a verified archive" 0 install_from "$good" "$pfx"
check "installed binary runs" 0 sh -c "[ \"\$('$pfx/bin/heavyq')\" = 'heavyq 9.9.9' ]"
check "no stray files in bin" 0 sh -c "[ \"\$(ls -A '$pfx/bin')\" = heavyq ]"
check "reinstall over an existing binary" 0 install_from "$good" "$pfx"

# 2. a tampered archive is refused and nothing is installed.
bad_rel="$work/rel-tampered"
cp -R "$good" "$bad_rel"
printf 'x' >>"$bad_rel/heavyq_9.9.9_${os}_${arch}.tar.gz"
pfx="$work/p2"
check "refuses a sha256 mismatch" fail install_from "$bad_rel" "$pfx"
check "nothing installed after a mismatch" fail test -e "$pfx/bin/heavyq"

# 3. an archive missing from checksums.txt is refused.
miss="$work/rel-missing"
cp -R "$good" "$miss"
grep -v "_${os}_${arch}" "$miss/checksums.txt" >"$miss/c" && mv "$miss/c" "$miss/checksums.txt"
check "refuses an archive not in checksums.txt" fail install_from "$miss" "$work/p3"

# 4. a duplicated checksum line is refused.
dup="$work/rel-dup"
cp -R "$good" "$dup"
grep "_${os}_${arch}" "$good/checksums.txt" >>"$dup/checksums.txt"
check "refuses a duplicated checksum line" fail install_from "$dup" "$work/p4"

# 5. missing download.
check "fails when the archive cannot be downloaded" fail install_from "$work/does-not-exist" "$work/p5"

# 6. argument validation.
check "rejects a malformed version" fail sh "$installer" --version "1.2" --prefix "$work/p6"
check "rejects a version with shell characters" fail sh "$installer" --version '1.2.3;touch x' --prefix "$work/p6"
check "rejects an unknown option" fail sh "$installer" --nope
check "base override needs --version" fail env HEAVYQ_RELEASE_BASE="file://$good" sh "$installer" --prefix "$work/p6"
check "--help exits 0" 0 sh "$installer" --help

# 7. --verify: needs cosign; passes the identity pin to it; fails closed when cosign says no.
cp "$good/checksums.txt" "$good/checksums.txt.sigstore.json"
stub="$work/stub"; mkdir -p "$stub"
printf '#!/bin/sh\necho "$@" >"%s/cosign-args"\nexit 0\n' "$work" >"$stub/cosign"
chmod 0755 "$stub/cosign"
check "--verify runs cosign" 0 env PATH="$stub:$PATH" HEAVYQ_RELEASE_BASE="file://$good" sh "$installer" --version 9.9.9 --prefix "$work/p7" --verify
check "--verify pins identity and issuer" 0 sh -c "grep -q 'certificate-identity-regexp' '$work/cosign-args' && grep -q 'token.actions.githubusercontent.com' '$work/cosign-args' && grep -q 'verify-blob' '$work/cosign-args'"
printf '#!/bin/sh\nexit 1\n' >"$stub/cosign"
check "--verify refuses a bad signature" fail env PATH="$stub:$PATH" HEAVYQ_RELEASE_BASE="file://$good" sh "$installer" --version 9.9.9 --prefix "$work/p8" --verify
check "nothing installed after a bad signature" fail test -e "$work/p8/bin/heavyq"
rm -f "$good/checksums.txt.sigstore.json"
check "--verify fails when the release has no signature" fail env PATH="$stub:$PATH" HEAVYQ_RELEASE_BASE="file://$good" sh "$installer" --version 9.9.9 --prefix "$work/p9" --verify

# 8. a real release (opt-in: needs network). v0.3.0 has an archive for each platform.
if [ "${HEAVYQ_TEST_NETWORK:-}" = 1 ]; then
  pfx="$work/real"
  check "installs a real release" 0 sh "$installer" --version 0.3.0 --prefix "$pfx"
  check "real binary reports its version" 0 sh -c "'$pfx/bin/heavyq' version | grep -q 0.3.0"
  check "installs the latest release" 0 sh "$installer" --prefix "$work/latest"
fi

if [ "$fails" -ne 0 ]; then printf '%s check(s) failed\n' "$fails"; exit 1; fi
echo "all install.sh checks passed"
