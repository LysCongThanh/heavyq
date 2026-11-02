#!/usr/bin/env bash
# Decides what a CI run has to do, from the files a pull request changes. Writes two outputs
# (to $GITHUB_OUTPUT, else stdout):
#
#   go    "true" unless the pull request changes only documentation (so the Go jobs can skip). Every other
#         event (push to main, schedule, dispatch) is "true". Anything not known to be documentation counts as code.
#   fuzz  JSON array of {pkg, targets} for the matrix of a fuzz job: on a pull request the packages it touched
#         (or all of them when it changes the fuzz plumbing); on any other event all of them.
#
# Needs: EVENT_NAME; for a pull request also GH_TOKEN, REPO (owner/name) and PR (number). The list of files comes
# from the API, so no history has to be checked out. A failure stops the script: the caller's job fails closed.
set -euo pipefail

out="${GITHUB_OUTPUT:-/dev/stdout}"
targets=".github/fuzz-targets.txt"

# Documentation and repository chores: nothing a Go job could notice. Everything else is code.
docs_only='^(docs/|changelog\.d/|\.github/ISSUE_TEMPLATE/|\.github/[^/]+\.md$|\.github/(CODEOWNERS|FUNDING\.yml)$|[^/]+\.md$|llms\.txt$|LICENSE$)'
# Changes to these retest every fuzz target.
fuzz_plumbing='^\.github/(workflows/ci\.yml|workflows/ci-full\.yml|fuzz-targets\.txt|scripts/run-fuzz\.sh)$'

all_targets() {
  jq -Rnc '[inputs | select(length > 0 and (startswith("#") | not)) | split(" ") | {pkg: .[0], targets: (.[1:] | join(" "))}]' "$targets"
}

if [ "${EVENT_NAME:-}" != "pull_request" ]; then
  echo "go=true" >> "$out"
  echo "fuzz=$(all_targets)" >> "$out"
  exit 0
fi

files="$(gh api "repos/${REPO}/pulls/${PR}/files" --paginate --jq '.[].filename')"

if printf '%s\n' "$files" | grep -Ev "$docs_only" | grep -q .; then go=true; else go=false; fi
echo "go=$go" >> "$out"

if printf '%s\n' "$files" | grep -Eq "$fuzz_plumbing"; then
  echo "fuzz=$(all_targets)" >> "$out"
else
  files_json="$(printf '%s\n' "$files" | jq -Rnc '[inputs | select(length > 0)]')"
  echo "fuzz=$(all_targets | jq -c --argjson f "$files_json" 'map(select(. as $e | any($f[]; startswith("internal/" + $e.pkg + "/"))))')" >> "$out"
fi
