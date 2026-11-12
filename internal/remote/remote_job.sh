#!/usr/bin/env bash
# heavyq remote job. Runs ON the remote machine, started by heavyq through the
# configured transport; not meant to be run by hand.
#   args:  <work_dir> <repo> <sha> <label> <slot>
#   stdin: the command line (shell syntax, base64, HEAVYQ_CMD_LEN bytes), then a lifeline
#   env:   HEAVYQ_CMD_LEN     length of the command line on stdin (it is not an argument: one exec argument
#                             holds at most 128 KiB, a command line as long as a local argv does not fit)
#          HEAVYQ_SUBDIR      subdirectory of the repo to run in ("" = root)
#          HEAVYQ_WITH        sibling repos "name@sha,name@sha"
#          HEAVYQ_BASE_REF / HEAVYQ_BASE_SHA   create refs/remotes/<ref> in the job's checkout
#          HEAVYQ_HOOK_B64    optional dependency-cache hook (shell, base64)
#          HEAVYQ_WATCH_STDIN=1  after the command line, stdin is a lifeline: EOF means heavyq died
#          HEAVYQ_ENV_PATH    directories put first on PATH ("~/" = $HOME; the remote's env_path)
# Each job checks out into a repository of its own under wt/ that borrows the bare repo's objects
# (alternates): its refs (the base ref) are nobody else's, and refs/heavyq/jobs/<job>/ in the bare repo keep
# its commits reachable while it runs. A job holds a flock on wt/<job>.lock for its whole life, so the next
# job reclaims what one that died without its cleanup (SIGKILL, a reboot) left behind.
# Portable: runs on Linux (flock, util-linux setsid) and on stock macOS (bash 3.2, Perl instead of flock/setsid).
# Exit: the command's code (97, 98 and 255 from the command become 96);
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

[[ "${HEAVYQ_CMD_LEN:-}" =~ ^[1-9][0-9]*$ ]] || fail "bad command length"
CMD_B64="$(head -c "$HEAVYQ_CMD_LEN")"
[[ "${#CMD_B64}" -eq "$HEAVYQ_CMD_LEN" ]] || fail "command line not received"

mkdir -p "$W/git" "$W/wt" "$W/slots" "$W/cache" || fail "cannot create $W"
exec 9> "$W/slots/$SLOT.lock" || fail "cannot open slot lock"
# Locks: flock(1) where it exists (Linux). macOS has none, but Perl ships with it and its flock() is the
# same flock(2) on the same open file description, so Linux and macOS locks behave identically.
lock_fd() { # <fd> <seconds>: an exclusive flock on an open descriptor, waiting up to <seconds>
  if command -v flock > /dev/null 2>&1; then flock -w "$2" "$1"; return; fi
  command -v perl > /dev/null 2>&1 || fail "need flock or perl for the locks"
  perl -MFcntl=:flock -e 'open(my $f, ">&=", $ARGV[0]) or exit 2; my $t = time + $ARGV[1];
    while (!flock($f, LOCK_EX | LOCK_NB)) { exit 1 if time >= $t; select(undef, undef, undef, 0.1) } exit 0' "$1" "$2"
}
lock_free() { # <file>: no process holds a flock on it
  if command -v flock > /dev/null 2>&1; then flock -n "$1" true 2> /dev/null; return; fi
  perl -MFcntl=:flock -e 'open(my $f, "<", $ARGV[0]) or exit 0; exit(flock($f, LOCK_EX | LOCK_NB) ? 0 : 1)' "$1" 2> /dev/null
}
lock_fd 9 5 || { echo "heavyq-job: slot $SLOT is busy (used outside the queue?)" >&2; exit 97; }
B64D=-d; printf QQ== | base64 -d > /dev/null 2>&1 || B64D=-D # BSD base64 before macOS 13 spells it -D

ID="${SHA:0:8}-$$"
NAME="$REPO-$ID"
BARE="$W/git/$REPO.git"
JOB="$W/wt/$NAME.d"
WT="$JOB/$REPO"
OWN="$W/wt/$NAME.lock"
CMDF="$W/wt/$NAME.cmd"
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

kill_group() { # TERM the process group, then KILL after 2 s
  local g="$1" i
  kill -0 -- "-$g" 2> /dev/null || return 0
  kill -TERM -- "-$g" 2> /dev/null
  for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 -- "-$g" 2> /dev/null || return 0; sleep 0.2; done
  kill -KILL -- "-$g" 2> /dev/null || true
}
CHILD=""; WATCH=""; PIN_BARES=(); PIN_REFS=()
cleanup() {
  trap '' TERM HUP INT
  if [[ -n "$WATCH" ]]; then pkill -P "$WATCH" 2> /dev/null; kill "$WATCH" 2> /dev/null; fi
  [[ -z "$CHILD" ]] || kill_group "$CHILD"
  local i
  for i in "${!PIN_REFS[@]}"; do git -C "${PIN_BARES[$i]}" update-ref -d "${PIN_REFS[$i]}" > /dev/null 2>&1; done
  [[ "$JOB" == "$W"/wt/"$REPO"-*.d && -d "$JOB" ]] && rm -rf -- "$JOB"
  rm -f -- "$CMDF" "$OWN"
  return 0
}
trap cleanup EXIT; trap 'exit 143' TERM; trap 'exit 129' HUP; trap 'exit 130' INT

# reclaim removes what jobs that died without their cleanup left under wt/: their lock is free. A directory
# without a lock file belongs to an earlier heavyq, which has none; it is left alone for two days.
reclaim() {
  local d o
  for d in "$W"/wt/*.d; do
    [[ -d "$d" && "$d" != "$JOB" ]] || continue
    o="${d%.d}.lock"
    if [[ -e "$o" ]]; then
      lock_free "$o" || continue
    elif [[ -z "$(find "$d" -maxdepth 0 -mmin +2880 2> /dev/null)" ]]; then
      continue
    fi
    rm -rf -- "$d"; rm -f -- "$o" "${d%.d}.cmd"
  done
  for o in "$W"/wt/*.lock; do
    [[ -e "$o" && "$o" != "$OWN" && ! -d "${o%.lock}.d" ]] && lock_free "$o" && rm -f -- "$o" "${o%.lock}.cmd"
  done
  git -C "$BARE" worktree prune > /dev/null 2>&1 # worktrees of an earlier heavyq whose directory is gone
  return 0
}
# prune_refs <bare> drops the pins of jobs that are gone and keeps the refs of the 100 most recently committed
# shipped commits. The kept ones make the next push small (it sends only what they do not reach); a later run
# of a dropped commit ships it again, which costs little when it is an ancestor of one kept.
prune_refs() {
  local b="$1" r n kept=0
  git -C "$b" for-each-ref --sort=-committerdate --format='%(refname)' refs/heavyq/ 2> /dev/null |
    while read -r r; do
      case "$r" in
        refs/heavyq/jobs/*)
          n="${r#refs/heavyq/jobs/}"; n="${n%%/*}"
          [[ -e "$W/wt/$n.lock" ]] && ! lock_free "$W/wt/$n.lock" && continue ;;
        *) kept=$((kept + 1)); [[ "$kept" -gt 100 ]] || continue ;;
      esac
      git -C "$b" update-ref -d "$r" > /dev/null 2>&1
    done
  return 0
}
# Under a short lock on wt.lock: take this job's own lock, then reclaim. A job is never seen half-made.
exec 6> "$W/wt.lock" || fail "cannot open $W/wt.lock"
lock_fd 6 30 || fail "work_dir is locked by another job for over 30 s"
exec 7> "$OWN" && lock_fd 7 5 || fail "cannot lock $OWN"
reclaim
prune_refs "$BARE"
for i in "${!SIB_NAMES[@]}"; do prune_refs "$W/git/${SIB_NAMES[$i]}.git"; done
exec 6>&-

if [[ "${HEAVYQ_WATCH_STDIN:-}" == 1 ]]; then
  # A background job's stdin defaults to /dev/null, so hand the lifeline over on fd 8.
  exec 8<&0
  ( cat <&8 > /dev/null 2>&1; kill -TERM $$ 2> /dev/null ) > /dev/null 2>&1 9>&- 7>&- &
  WATCH=$!
  exec 8<&-
fi
exec < /dev/null

pin() { # <bare> <what> <sha>: keep <sha> reachable in <bare> while this job runs
  git -C "$1" update-ref "refs/heavyq/jobs/$NAME/$2" "$3" > /dev/null 2>&1 || return 1
  PIN_BARES+=("$1"); PIN_REFS+=("refs/heavyq/jobs/$NAME/$2")
}
checkout() { # <bare> <dir> <sha>: a repository of its own at <dir>, borrowing <bare>'s objects, detached at <sha>
  # Hooks off in its own config, as in the bare repo (push.go): every later git in it (checkout, the base
  # ref's update-ref) skips the machine's hooks, which do not reach it through the borrowed objects.
  git -c core.hooksPath=/dev/null init -q --template= "$2" > /dev/null 2>&1 || return 1
  git -C "$2" config core.hooksPath /dev/null || return 1
  mkdir -p "$2/.git/objects/info" && printf '%s\n' "$1/objects" > "$2/.git/objects/info/alternates" || return 1
  git -C "$2" -c advice.detachedHead=false checkout -q --detach "$3"
}
[[ -d "$BARE" ]] || fail "no bare repo $BARE (push first)"
git -C "$BARE" cat-file -e "$SHA^{commit}" 2> /dev/null || fail "commit $SHA not in $BARE"
pin "$BARE" head "$SHA" || fail "cannot pin $SHA in $BARE"
mkdir -p "$JOB" || fail "cannot create $JOB"
checkout "$BARE" "$WT" "$SHA" 9>&- 7>&- || fail "cannot check out $SHA"
if [[ -n "$BASE_REF" ]]; then
  pin "$BARE" base "$BASE_SHA" || fail "base commit $BASE_SHA not in $BARE"
  git -C "$WT" update-ref "refs/remotes/$BASE_REF" "$BASE_SHA" || fail "cannot create base ref $BASE_REF"
fi
for i in "${!SIB_NAMES[@]}"; do
  SB="$W/git/${SIB_NAMES[$i]}.git"
  git -C "$SB" cat-file -e "${SIB_SHAS[$i]}^{commit}" 2> /dev/null || fail "commit ${SIB_SHAS[$i]} not in $SB"
  pin "$SB" "with-$i" "${SIB_SHAS[$i]}" || fail "cannot pin ${SIB_SHAS[$i]} in $SB"
  checkout "$SB" "$JOB/${SIB_NAMES[$i]}" "${SIB_SHAS[$i]}" 9>&- 7>&- || fail "cannot check out sibling ${SIB_NAMES[$i]}"
done
SUB="${HEAVYQ_SUBDIR:-}"
if [[ -n "$SUB" ]]; then
  [[ "$SUB" =~ ^[A-Za-z0-9._/-]+$ && "$SUB" != *..* && -d "$WT/$SUB" ]] || fail "bad subdirectory '$SUB'"
fi
cd "$WT/$SUB" || fail "cannot enter the checkout"
echo "heavyq-job: $(hostname 2> /dev/null || echo remote): $REPO@${SHA:0:12}, lane $LABEL, slot $SLOT" >&2
export HEAVYQ_JOB_ID="$ID" HEAVYQ_WORKTREE="$WT" HEAVYQ_CACHE_DIR="$W/cache"

# isolated starts "$@" in the background, in a session and process group of its own, and sets CHILD to its
# pid: a stop request interrupts the `wait` for it at once (a trap waits for a foreground command), and
# kill_group reaches everything it started. 9>&- 7>&-: it never inherits the slot or the job lock, so
# daemons it leaves behind cannot pin them. util-linux setsid(1) on Linux; elsewhere (macOS has none) Perl
# calls setsid(2) and execs (same pid, so $! is still the group id that kill_group signals). Any other program
# named setsid is not trusted: a wrapper that forks and exits hands `wait` an early 0 and leaves the command
# running after its checkout is gone. heavyq's exit canary checks the result of this choice (docs/remote.md).
isolated() {
  if command -v setsid > /dev/null 2>&1 && setsid --version 2> /dev/null < /dev/null 9>&- 7>&- | grep util-linux > /dev/null; then
    setsid "$@" 9>&- 7>&- &
  else
    command -v perl > /dev/null 2>&1 || fail "need util-linux setsid or perl to isolate the command"
    perl -MPOSIX -e 'POSIX::setsid() >= 0 or die "heavyq-job: setsid: $!\n"; exec @ARGV or die "heavyq-job: exec: $!\n"' "$@" 9>&- 7>&- &
  fi
  CHILD=$!
}
if [[ -n "${HEAVYQ_HOOK_B64:-}" ]]; then
  HOOK="$(printf '%s' "$HEAVYQ_HOOK_B64" | base64 "$B64D")" || fail "cannot decode cache hook"
  isolated bash -c "$HOOK"
  wait "$CHILD"; rc=$?; CHILD=""
  [[ "$rc" -eq 0 ]] || fail "cache hook failed"
fi
{ printf '%s' "$CMD_B64" | base64 "$B64D" > "$CMDF"; } 2> /dev/null && [[ -s "$CMDF" ]] || fail "cannot decode command"
# The job's own inputs are not the command's: a heavyq (or a test of heavyq's job script) run by the
# command would otherwise read this job's base ref, siblings or command as its own.
unset CMD_B64 HEAVYQ_CMD_LEN HEAVYQ_SUBDIR HEAVYQ_WITH HEAVYQ_BASE_REF HEAVYQ_BASE_SHA HEAVYQ_HOOK_B64 HEAVYQ_WATCH_STDIN HEAVYQ_ENV_PATH
isolated bash "$CMDF"
wait "$CHILD"; rc=$?
kill_group "$CHILD"; CHILD=""
if [[ "$rc" -eq 97 || "$rc" -eq 98 || "$rc" -eq 255 ]]; then
  echo "heavyq-job: the command exited $rc, reported as 96 (97, 98 and 255 are heavyq's own codes)" >&2
  rc=96
fi
exit "$rc"
