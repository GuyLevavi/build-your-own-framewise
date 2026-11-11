#!/usr/bin/env bash
# Start an attempt in a language: attempts/<lang> becomes a copy of compiled_starters/<lang>,
# on the first stage. Then ./test.sh <lang>.
#
#   tools/start.sh <lang>
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
lang="${1:?usage: tools/start.sh <lang> (one of: $(ls "$root/compiled_starters" | tr '\n' ' '))}"
[[ -d "$root/compiled_starters/$lang" ]] || { echo "no starter for $lang (have: $(ls "$root/compiled_starters" | tr '\n' ' '))" >&2; exit 2; }
[[ ! -e "$root/attempts/$lang" ]] || { echo "attempts/$lang already exists" >&2; exit 1; }
mkdir -p "$root/attempts"
cp -a "$root/compiled_starters/$lang" "$root/attempts/$lang"
chmod -R u+w "$root/attempts/$lang"
awk '/^stages:/{s=1} s&&/^  - slug:/{gsub(/.*slug: *"|".*/,""); print; exit}' "$root/course-definition.yml" >"$root/attempts/$lang/.stage"
echo "attempts/$lang is ready, on stage $(cat "$root/attempts/$lang/.stage"). Run ./test.sh $lang"
