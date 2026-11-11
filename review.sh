#!/usr/bin/env bash
# The reviewer's fast track on attempts/python (tools/fast_track.sh and tools/brief.py).
#
#   ./review.sh <slug>                 attempts/python/app (and the delivery files) become the
#                                      reference solution of the stage before <slug>, its .stage
#                                      becomes <slug>, and the brief opens (commit your work first)
#   ./review.sh --brief [slug]         the brief: task, what the reference built, what to review,
#                                      what the stage points to (default: current stage)
#   ./review.sh --diff [slug]          the brief with the reference diff
#   ./review.sh --list                 every stage, ★ on those worth doing yourself
#
# Then ./test.sh runs the stage, as the learner would.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
attempt="$root/attempts/python"
current="$(cat "$attempt/.stage")"
python="$(command -v python3 || true)"
if [[ -z "$python" ]]; then
  exec nix develop "$root#python" --command "$0" "$@"
fi

cd "$root"
brief() {
  if [[ -t 1 ]]; then
    "$python" tools/brief.py "$@" | "${PAGER:-less}"
  else
    "$python" tools/brief.py "$@"
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
    "$python" tools/brief.py --list
    ;;
  "" | -*)
    sed -n '2,13s/^# \{0,1\}//p' "$0" >&2
    exit 2
    ;;
  *)
    exec bash tools/fast_track.sh "$1" "$attempt"
    ;;
esac
