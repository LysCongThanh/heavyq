#!/usr/bin/env bash
# Fuzzes the targets of one package for FUZZTIME each: run-fuzz.sh <pkg under internal/> "<Target> ..." <fuzztime>.
# A crasher fails the run and is left under internal/<pkg>/testdata/fuzz for the workflow to upload.
set -euo pipefail

pkg="$1" targets="$2" fuzztime="$3"
for target in $targets; do
  echo "::group::fuzz ${pkg} ${target} (${fuzztime})"
  go test "./internal/${pkg}" -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime" -fuzzminimizetime 10s
  echo "::endgroup::"
done
