#!/usr/bin/env bash
# heavyq remote job. Runs ON the remote machine, started by heavyq through the
# configured transport; not meant to be run by hand.
#   args: <work_dir> <repo> <sha> <label> <slot>
#   env:  HEAVYQ_CMD_B64     command line (shell syntax, base64)
#         HEAVYQ_SUBDIR      subdirectory of the repo to run in ("" = root)
#         HEAVYQ_WITH        sibling repos "name@sha,name@sha"
#         HEAVYQ_BASE_REF / HEAVYQ_BASE_SHA   recreate refs/remotes/<ref>
#         HEAVYQ_HOOK_B64    optional dependency-cache hook (shell, base64)
#         HEAVYQ_WATCH_STDIN=1  stdin is a lifeline: EOF means heavyq died
#         HEAVYQ_ENV_PATH    directories put first on PATH ("~/" = $HOME; the remote's env_path)
# Portable: runs on Linux (flock, util-linux setsid) and on stock macOS (bash 3.2, Perl instead of flock/setsid).
# Exit: the command's code (97/98 from the command become 96);
#       97 = slot busy, 98 = setup failed (nothing was run).
set -uo pipefail
W="$1"; REPO="$2"; SHA="$3"; LABEL="$4"; SLOT="$5"
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
safe() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; }
fail() { echo "heavyq-job: $*" >&2; exit 98; }
safe "$REPO" && [[ "$SHA" =~ ^[0-9a-f]{40}$ ]] && safe "$LABEL" && safe "$SLOT" || fail "bad arguments"

# PATH. macOS: a non-interactive ssh command gets a bare PATH, so ask the user's login shell for theirs
# (a hung profile is cut off after 10 s). Both: HEAVYQ_ENV_PATH (the remote's env_path) goes first; "~/" is $HOME.
# The tool probe (tool_probe.go) runs this block, between the two markers, as it is: keep it self-contained.
# >>> job PATH
if [[ "$(uname -s 2> /dev/null)" == Darwin ]]; then
  _sh="${SHELL:-/bin/zsh}"; case "${_sh##*/}" in zsh | bash) ;; *) _sh=/bin/zsh ;; esac
  _lp="$({ command -v perl > /dev/null 2>&1 && perl -e 'alarm 10; exec @ARGV' "$_sh" -lc 'printf "\nheavyq-path:%s\n" "$PATH"'; } 2> /dev/null < /dev/null | sed -n 's/^heavyq-path://p' | tail -n 1)"
  [[ -z "$_lp" ]] || PATH="$_lp:$PATH"
fi
if [[ -n "${HEAVYQ_ENV_PATH:-}" ]]; then
  _pre=""; IFS=: read -r -a _eps <<< "$HEAVYQ_ENV_PATH"
  for _d in ${_eps[@]+"${_eps[@]}"}; do
    [[ -n "$_d" ]] || continue
    case "$_d" in "~") _d="$HOME" ;; "~/"*) _d="$HOME/${_d#"~/"}" ;; esac
    _pre="$_pre$_d:"
  done
  PATH="$_pre$PATH"
fi
export PATH
# <<< job PATH

mkdir -p "$W/git" "$W/wt" "$W/slots" "$W/cache" || fail "cannot create $W"
exec 9> "$W/slots/$SLOT.lock" || fail "cannot open slot lock"
# Slot lock: flock(1) where it exists (Linux). macOS has none, but Perl ships with it and its flock() is the
# same flock(2) on the same open file description, so Linux and macOS slots behave identically.
lock_slot() {
  if command -v flock > /dev/null 2>&1; then flock -w 5 9; return; fi
  command -v perl > /dev/null 2>&1 || fail "need flock or perl for the slot lock"
  perl -MFcntl=:flock -e 'open(my $f, ">&=", 9) or exit 2; my $t = time + 5;
    while (!flock($f, LOCK_EX | LOCK_NB)) { exit 1 if time >= $t; select(undef, undef, undef, 0.1) } exit 0'
}
lock_slot || { echo "heavyq-job: slot $SLOT is busy (used outside the queue?)" >&2; exit 97; }
B64D=-d; printf QQ== | base64 -d > /dev/null 2>&1 || B64D=-D # BSD base64 before macOS 13 spells it -D

ID="${SHA:0:8}-$$"
BARE="$W/git/$REPO.git"
JOB="$W/wt/$REPO-$ID.d"
WT="$JOB/$REPO"
SIB_NAMES=(); SIB_SHAS=()
if [[ -n "${HEAVYQ_WITH:-}" ]]; then
  IFS=, read -r -a _sibs <<< "$HEAVYQ_WITH"
  for _s in "${_sibs[@]}"; do
    _n="${_s%%@*}"; _h="${_s#*@}"
    safe "$_n" && [[ "$_n" != "$REPO" && "$_h" =~ ^[0-9a-f]{40}$ ]] || fail "bad HEAVYQ_WITH entry '$_s'"
    SIB_NAMES+=("$_n"); SIB_SHAS+=("$_h")
  done
fi
BASE_REF="${HEAVYQ_BASE_REF:-}"; BASE_SHA="${HEAVYQ_BASE_SHA:-}"
if [[ -n "$BASE_REF" ]]; then
  [[ "$BASE_REF" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$ && "$BASE_REF" != *..* && "$BASE_SHA" =~ ^[0-9a-f]{40}$ ]] || fail "bad base ref"
fi

kill_group() { # TERM the command's process group, then KILL after 2 s
  local g="$1" i
  kill -0 -- "-$g" 2> /dev/null || return 0
  kill -TERM -- "-$g" 2> /dev/null
  for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 -- "-$g" 2> /dev/null || return 0; sleep 0.2; done
  kill -KILL -- "-$g" 2> /dev/null || true
}
CHILD=""; WATCH=""
cleanup() {
  trap '' TERM HUP INT
  if [[ -n "$WATCH" ]]; then pkill -P "$WATCH" 2> /dev/null; kill "$WATCH" 2> /dev/null; fi
  [[ -z "$CHILD" ]] || kill_group "$CHILD"
  git -C "$BARE" worktree remove --force "$WT" > /dev/null 2>&1
  local i
  for i in "${!SIB_NAMES[@]}"; do
    git -C "$W/git/${SIB_NAMES[$i]}.git" worktree remove --force "$JOB/${SIB_NAMES[$i]}" > /dev/null 2>&1
    git -C "$W/git/${SIB_NAMES[$i]}.git" worktree prune > /dev/null 2>&1
  done
  [[ "$JOB" == "$W"/wt/"$REPO"-*.d && -d "$JOB" ]] && rm -rf -- "$JOB"
  git -C "$BARE" worktree prune > /dev/null 2>&1
  # the base ref is shared by jobs of the same repo: drop it only when no other worktree uses it
  if [[ -n "$BASE_REF" && "$(git -C "$BARE" worktree list 2> /dev/null | wc -l)" -le 1 ]]; then
    git -C "$BARE" update-ref -d "refs/remotes/$BASE_REF" "$BASE_SHA" > /dev/null 2>&1
  fi
  return 0
}
trap cleanup EXIT; trap 'exit 143' TERM; trap 'exit 129' HUP; trap 'exit 130' INT
if [[ "${HEAVYQ_WATCH_STDIN:-}" == 1 ]]; then
  # A background job's stdin defaults to /dev/null, so hand the lifeline over on fd 8.
  exec 8<&0
  ( cat <&8 > /dev/null 2>&1; kill -TERM $$ 2> /dev/null ) > /dev/null 2>&1 9>&- &
  WATCH=$!
  exec 8<&-
fi
exec < /dev/null

[[ -d "$BARE" ]] || fail "no bare repo $BARE (push first)"
git -C "$BARE" cat-file -e "$SHA^{commit}" 2> /dev/null || fail "commit $SHA not in $BARE"
mkdir -p "$JOB" || fail "cannot create $JOB"
git -C "$BARE" worktree add --detach -q "$WT" "$SHA" 9>&- || fail "cannot create worktree"
if [[ -n "$BASE_REF" ]]; then
  git -C "$BARE" update-ref "refs/remotes/$BASE_REF" "$BASE_SHA" || fail "cannot create base ref $BASE_REF"
fi
for i in "${!SIB_NAMES[@]}"; do
  SB="$W/git/${SIB_NAMES[$i]}.git"
  git -C "$SB" cat-file -e "${SIB_SHAS[$i]}^{commit}" 2> /dev/null || fail "commit ${SIB_SHAS[$i]} not in $SB"
  git -C "$SB" worktree add --detach -q "$JOB/${SIB_NAMES[$i]}" "${SIB_SHAS[$i]}" 9>&- || fail "cannot create sibling worktree ${SIB_NAMES[$i]}"
done
SUB="${HEAVYQ_SUBDIR:-}"
if [[ -n "$SUB" ]]; then
  [[ "$SUB" =~ ^[A-Za-z0-9._/-]+$ && "$SUB" != *..* && -d "$WT/$SUB" ]] || fail "bad subdirectory '$SUB'"
fi
cd "$WT/$SUB" || fail "cannot enter worktree"
echo "heavyq-job: $(hostname 2> /dev/null || echo remote): $REPO@${SHA:0:12}, lane $LABEL, slot $SLOT" >&2
export HEAVYQ_JOB_ID="$ID" HEAVYQ_WORKTREE="$WT" HEAVYQ_CACHE_DIR="$W/cache"
if [[ -n "${HEAVYQ_HOOK_B64:-}" ]]; then
  HOOK="$(printf '%s' "$HEAVYQ_HOOK_B64" | base64 "$B64D")" || fail "cannot decode cache hook"
  bash -c "$HOOK" 9>&- || fail "cache hook failed"
fi
CMD="$(printf '%s' "${HEAVYQ_CMD_B64:-}" | base64 "$B64D")" || fail "cannot decode command"
[[ -n "$CMD" ]] || fail "empty command"
# The job's own inputs are not the command's: a heavyq (or a test of heavyq's job script) run by the
# command would otherwise read this job's base ref, siblings or command as its own.
unset HEAVYQ_CMD_B64 HEAVYQ_SUBDIR HEAVYQ_WITH HEAVYQ_BASE_REF HEAVYQ_BASE_SHA HEAVYQ_HOOK_B64 HEAVYQ_WATCH_STDIN HEAVYQ_ENV_PATH
# 9>&-: the command never inherits the slot lock, so daemons it leaves behind cannot pin the slot.
# Own session and process group: util-linux setsid(1) on Linux; elsewhere (macOS has none) Perl calls setsid(2)
# and execs bash (same pid, so $! is still the group id that kill_group signals). Any other program named setsid
# is not trusted: a wrapper that forks and exits hands `wait` an early 0 and leaves the command running after its
# worktree is gone. heavyq's exit canary checks the result of this choice (docs/remote.md).
if command -v setsid > /dev/null 2>&1 && setsid --version 2> /dev/null < /dev/null 9>&- | grep util-linux > /dev/null; then
  setsid bash -c "$CMD" 9>&- &
else
  command -v perl > /dev/null 2>&1 || fail "need util-linux setsid or perl to isolate the command"
  perl -MPOSIX -e 'POSIX::setsid() >= 0 or die "heavyq-job: setsid: $!\n"; exec @ARGV or die "heavyq-job: exec: $!\n"' bash -c "$CMD" 9>&- &
fi
CHILD=$!
wait "$CHILD"; rc=$?
kill_group "$CHILD"; CHILD=""
[[ "$rc" -ne 97 && "$rc" -ne 98 ]] || rc=96
exit "$rc"
