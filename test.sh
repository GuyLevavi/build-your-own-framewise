#!/usr/bin/env bash
# Run the Build your own Framewise tester against your attempt in attempts/<lang>.
#
#   ./test.sh [lang]                all stages up to the one in attempts/<lang>/.stage
#   ./test.sh [lang] <slug>         a single stage
#   ./test.sh [lang] --list         every stage in course order (* = current)
#   ./test.sh [lang] --next         advance .stage to the following stage
#   ./test.sh [lang] --show [slug]  print a stage's instructions (default: current)
#
# lang can be left out while attempts/ holds a single language. The tests run in the flake's
# devShell for that language (nix develop .#<lang>), with the tester built from ./tester.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
course="$root/course-definition.yml"

mapfile -t langs < <(cd "$root/attempts" && ls -d */ 2>/dev/null | tr -d /)
if [[ -n "${1:-}" && -d "$root/attempts/$1" ]]; then
  lang="$1"
  shift
elif [[ ${#langs[@]} -eq 1 ]]; then
  lang="${langs[0]}"
else
  echo "which language? ./test.sh <lang> ..., one of: ${langs[*]:-(none; start one with tools/start.sh <lang>)}" >&2
  exit 2
fi
here="$root/attempts/$lang"
stage_file="$here/.stage"

# ---- per-language part -------------------------------------------------------
# Build up front with visible output so compile errors aren't buried in tester logs.
build() {
  sh .codecrafters/compile.sh
}
# Anything the tester needs beyond the CODECRAFTERS_* variables.
prepare() {
  :
}
# ---- end of per-language part ------------------------------------------------

# Toolchain and tester come from the devShell (pinned in flake.lock).
if [[ -z "${FRAMEWISE_TESTER_BIN:-}" ]]; then
  exec nix develop "$root#$lang" --command "$0" "$lang" "$@"
fi

# "slug<TAB>name" per stage, in course order.
stages() {
  awk '
    /^stages:/ { in_stages = 1; next }
    !in_stages { next }
    /^  - slug:/ { slug = $3; gsub(/["\047]/, "", slug) }
    /^    name:/ && slug != "" {
      name = $0; sub(/^    name: */, "", name); gsub(/^["\047]|["\047]$/, "", name)
      print slug "\t" name; slug = ""
    }
  ' "$course"
}

current="$(cat "$stage_file" 2>/dev/null || stages | head -n1 | cut -f1)"

case "${1:-}" in
  --list)
    stages | awk -F'\t' -v cur="$current" '{ printf "%s %3d  %s  %s\n", ($1 == cur ? "*" : " "), NR, $1, $2 }'
    exit 0
    ;;
  --next)
    next="$(stages | awk -F'\t' -v cur="$current" 'found { print $1; exit } $1 == cur { found = 1 }')"
    [[ -n "$next" ]] || { echo "already at the last stage ($current)" >&2; exit 1; }
    echo "$next" > "$stage_file"
    stages | awk -F'\t' -v s="$next" '$1 == s { print "now on " $1 ": " $2 }'
    exit 0
    ;;
  --show)
    desc=("$root"/stage_descriptions/*-"${2:-$current}".md)
    [[ -f "${desc[0]}" ]] || { echo "no instructions for stage '${2:-$current}'" >&2; exit 1; }
    # render-markdown needs a .md filetype, and leaves the course's inline <code> HTML raw.
    md="$(mktemp --suffix=.md)"
    trap 'rm -f "$md"' EXIT
    {
      stages | awk -F'\t' -v s="${2:-$current}" '$1 == s { printf "# Stage #%d: %s\n\n", NR, $2 }'
      sed -E 's#<code[^>]*>([^<]*)</code>#`\1`#g' "${desc[0]}"
    } > "$md"
    if [[ -t 1 ]] && command -v nvim >/dev/null; then
      nvim -R -c 'nnoremap <buffer> q <cmd>qa<cr>' "$md"
    else
      cat "$md"
    fi
    exit 0
    ;;
  -*)
    sed -n '2,11s/^# \{0,1\}//p' "$0" >&2
    exit 2
    ;;
  "")
    selected="$(stages | awk -F'\t' -v cur="$current" '{ print } $1 == cur { exit }')"
    ;;
  *)
    selected="$(stages | awk -F'\t' -v s="$1" '$1 == s')"
    ;;
esac

if [[ -z "$selected" ]]; then
  echo "unknown stage '${1:-$current}' (see ./test.sh $lang --list)" >&2
  exit 1
fi

# The tester's JSON titles carry the course-wide stage number, as on codecrafters.io.
json="$(stages | awk -F'\t' -v sel="$(cut -f1 <<<"$selected" | paste -sd' ')" '
  BEGIN { n = split(sel, s, " "); for (i = 1; i <= n; i++) want[s[i]] = 1 }
  want[$1] {
    name = $2; gsub(/\\/, "\\\\", name); gsub(/"/, "\\\"", name)
    printf "%s{\"slug\":\"%s\",\"tester_log_prefix\":\"tester::#%s\",\"title\":\"Stage #%d: %s\"}",
      (out++ ? "," : ""), $1, toupper($1), NR, name
  }
')"

# The tester runs from the attempt, as CodeCrafters runs it from /app.
cd "$here"
build
prepare

exec env \
  TESTER_DIR="$root/tester" \
  CODECRAFTERS_REPOSITORY_DIR="$here" \
  CODECRAFTERS_TEST_CASES_JSON="[$json]" \
  CODECRAFTERS_SKIP_ANTI_CHEAT=true \
  "$FRAMEWISE_TESTER_BIN"
