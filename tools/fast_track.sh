#!/usr/bin/env bash
# The reviewer's fast track: start any stage from the reference solution of the one before it.
#
#   tools/fast_track.sh STAGE_SLUG DIR
#
# DIR is your attempt (created from compiled_starters/python if missing). Its app/ is replaced by
# the reference solution of the stage before STAGE_SLUG (the starter's, for the first stage), so
# you can do just STAGE_SLUG, then compare with that stage's reference diff. Commit first if DIR
# holds work you want to keep.
set -euo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
slug="${1:?usage: tools/fast_track.sh STAGE_SLUG DIR}"
dir="${2:?usage: tools/fast_track.sh STAGE_SLUG DIR}"

mapfile -t slugs < <(awk '/^stages:/{s=1} s&&/^  - slug:/{gsub(/.*slug: *"|".*/,""); print}' "$course_dir/course-definition.yml")
n=0
for i in "${!slugs[@]}"; do
  [[ "${slugs[$i]}" == "$slug" ]] && n=$((i + 1))
done
[[ "$n" -gt 0 ]] || { echo "unknown stage: $slug (have: ${slugs[*]})" >&2; exit 2; }

if [[ ! -d "$dir" ]]; then
  cp -r "$course_dir/compiled_starters/python" "$dir"
  echo "created $dir from the starter"
fi
if [[ "$n" == 1 ]]; then
  from="$course_dir/compiled_starters/python/app"
else
  from="$course_dir/solutions/python/$(printf %02d $((n - 1)))-${slugs[$((n - 2))]}/code/app"
fi
rm -rf "$dir/app"
cp -r "$from" "$dir/app"
if [[ "$n" == 1 ]]; then
  echo "$dir/app is now the starter's"
else
  echo "$dir/app is now the reference solution of stage $((n - 1)) (${slugs[$((n - 2))]})"
fi
echo
echo "Your stage:     $course_dir/stage_descriptions/base-$(printf %02d "$n")-$slug.md"
echo "Test it:        $course_dir/tools/run_tests.sh $dir $slug"
echo "Then compare:   $course_dir/solutions/python/$(printf %02d "$n")-$slug/diff/"
