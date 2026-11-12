#!/usr/bin/env bash
# Prove every stage is sound (the brief's definition of done):
#   1. solution N passes stages 1..N (later stages never break earlier ones);
#   2. stage N fails against solution N-1 (and stage 1 fails against the starter).
#
#   tools/verify_course.sh [FIRST_STAGE_NUMBER] [LAST_STAGE_NUMBER]
#
# Logs go to tools/.verify/; the summary goes to stdout. Exit code 0 only if every check holds.
set -uo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$course_dir"
logs="$course_dir/tools/.verify"
mkdir -p "$logs"

mapfile -t slugs < <(awk '/^stages:/{s=1} s&&/^  - slug:/{gsub(/.*slug: *"|".*/,""); print}' course-definition.yml)
first="${1:-1}"
last="${2:-${#slugs[@]}}"
failures=0

for n in $(seq "$first" "$last"); do
  slug="${slugs[$((n - 1))]}"
  solution="solutions/python/$(printf %02d "$n")-$slug/code"
  if [[ "$n" == 1 ]]; then
    previous="compiled_starters/python"
  else
    previous="solutions/python/$(printf %02d $((n - 1)))-${slugs[$((n - 2))]}/code"
  fi

  if tools/run_tests.sh "$solution" "$slug" ${VERIFY_ONLY:+--only} >"$logs/$n-$slug-pass.log" 2>&1; then
    pass="ok  "
  else
    why="$(grep -a -v 'Test failed' "$logs/$n-$slug-pass.log" | grep -a -o $'\e\\[91m.*' | head -1 | sed $'s/\e\\[[0-9;]*m//g')"
    pass="FAIL ($(grep -a -o '^\[stage-[0-9]*\]' "$logs/$n-$slug-pass.log" | tail -1): ${why:0:110})"
    failures=$((failures + 1))
  fi
  if tools/run_tests.sh "$previous" "$slug" --only >"$logs/$n-$slug-previous.log" 2>&1; then
    prev="PASSES (stage proves nothing)"
    failures=$((failures + 1))
  else
    reason="$(grep -a -v 'Test failed' "$logs/$n-$slug-previous.log" | grep -a -o $'\e\\[91m.*' | head -1 | sed $'s/\e\\[[0-9;]*m//g')"
    prev="fails as expected: ${reason:0:110}"
  fi
  printf "%2d %-4s solution passes 1..%-2d %s | previous solution: %s\n" "$n" "$slug" "$n" "$pass" "$prev"
done

exit $((failures > 0))
