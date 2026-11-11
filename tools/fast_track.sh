#!/usr/bin/env bash
# The reviewer's fast track: start any stage from the reference solution of the one before it.
#
#   tools/fast_track.sh STAGE_SLUG DIR
#
# DIR is your attempt (created from compiled_starters/python if missing). Its app/ (and, for the
# delivery stages, tests/, .gitlab-ci.yml, Containerfile, .containerignore and deploy/) is replaced
# by the reference solution of the stage before STAGE_SLUG (the starter's, for the first stage), so
# you can do just STAGE_SLUG, then compare with that stage's reference diff. Commit first if DIR
# holds work you want to keep. If DIR has a .stage (attempts/python, which ./review.sh passes),
# .stage becomes STAGE_SLUG, so ./test.sh runs up to it.
#
# Last, it prints the stage's brief (tools/brief.py): the task, what the reference built, what to
# look for in review and what the stage points to. Set FAST_TRACK_BRIEF=0 to skip it.
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
  chmod -R u+w "$dir" # the course may be read-only (a nix store path)
  echo "created $dir from the starter"
fi
if [[ "$n" == 1 ]]; then
  from="$course_dir/compiled_starters/python"
else
  from="$course_dir/solutions/python/$(printf %02d $((n - 1)))-${slugs[$((n - 2))]}/code"
fi
# What the learner writes (the same list as tools/compile.sh).
for name in app tests .gitlab-ci.yml Containerfile .containerignore deploy; do
  if [[ -e "$from/$name" ]]; then
    rm -rf "${dir:?}/$name"
    cp -r "$from/$name" "$dir/$name"
    chmod -R u+w "$dir/$name"
  else
    # Not written yet at that stage (going back from a later one leaves it behind otherwise).
    rm -rf "${dir:?}/$name"
  fi
done
[[ -f "$dir/.stage" ]] && echo "$slug" >"$dir/.stage"
if [[ "$n" == 1 ]]; then
  echo "$dir/app is now the starter's"
else
  echo "$dir now holds the reference solution of stage $((n - 1)) (${slugs[$((n - 2))]})"
fi
echo
echo "Your stage:     $course_dir/stage_descriptions/base-$(printf %02d "$n")-$slug.md"
if [[ "$(cd "$dir" && pwd)" == "$course_dir/attempts/python" ]]; then
  echo "Test it:        ./test.sh"
else
  echo "Test it:        $course_dir/tools/run_tests.sh $dir $slug"
fi
echo "Then compare:   $course_dir/solutions/python/$(printf %02d "$n")-$slug/diff/"
echo "The brief:      $course_dir/tools/brief.py $slug [--diff]"

if [[ "${FAST_TRACK_BRIEF:-1}" != 0 ]]; then
  if [[ -t 1 ]]; then
    python3 "$course_dir/tools/brief.py" "$slug" | "${PAGER:-less}"
  else
    echo
    python3 "$course_dir/tools/brief.py" "$slug"
  fi
fi
