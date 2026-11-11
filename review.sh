#!/usr/bin/env bash
# The reviewer's fast track, with the course pinned in flake.lock (its tools/fast_track.sh and
# tools/brief.py). Clone framewise next to this repo for the assignment part of the brief.
#
#   ./review.sh <slug>                 app/ (and the delivery files) become the reference solution
#                                      of the stage before <slug>, .stage becomes <slug>, and the
#                                      brief opens (commit your work first)
#   ./review.sh --brief [slug] [--full]  the brief: task, what the reference built, what to review,
#                                      the Framewise weeks (default: current stage)
#   ./review.sh --diff [slug]          the brief with the reference diff
#   ./review.sh --list                 every stage with its Framewise weeks and ★
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
if [[ -z "${FRAMEWISE_COURSE_DIR:-}" ]]; then
  exec nix develop "$here" --command "$0" "$@"
fi
course_dir="$FRAMEWISE_COURSE_DIR"
current="$(cat "$here/.stage")"

# Run from the repo, so the brief finds ../framewise.
cd "$here"
brief() {
  if [[ -t 1 ]]; then
    python3 "$course_dir/tools/brief.py" "$@" | "${PAGER:-less}"
  else
    python3 "$course_dir/tools/brief.py" "$@"
  fi
}

case "${1:-}" in
  --brief | --diff)
    mode="$1"
    shift
    slug="$current"
    if [[ $# -gt 0 && "$1" != -* ]]; then
      slug="$1"
      shift
    fi
    [[ "$mode" == --diff ]] && set -- --diff "$@"
    brief "$slug" "$@"
    ;;
  --list)
    python3 "$course_dir/tools/brief.py" --list
    ;;
  "" | -*)
    sed -n '2,12s/^# \{0,1\}//p' "$0" >&2
    exit 2
    ;;
  *)
    exec bash "$course_dir/tools/fast_track.sh" "$1" "$here"
    ;;
esac
