#!/usr/bin/env bash
# Run the Build your own Framewise tester locally.
#
#   ./test.sh           all stages up to the one in .stage
#   ./test.sh <slug>    a single stage
#   ./test.sh --list    every stage in course order (* = current)
#   ./test.sh --next    advance .stage to the following stage
#   ./test.sh --show [slug]  print a stage's instructions (default: current)
#   ./test.sh --skip-to <slug>  fast track: app/ becomes the reference solution of the stage
#                     before <slug>, and .stage becomes <slug> (commit your work first)
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
stage_file="$here/.stage"

# ---- challenge-specific (build-your-own-y PROTOCOL.md, "test.sh") ----------
# The devShell exports ${prefix}_COURSE_DIR, ${prefix}_TESTER_BIN and ${prefix}_TESTER_SRC.
prefix=FRAMEWISE
# The flake whose devShell runs the tests.
flake="$here"

# Build up front with visible output so compile errors aren't buried in tester logs.
build() {
  sh .codecrafters/compile.sh
}

# Anything the tester needs beyond the CODECRAFTERS_* variables. TESTER_DIR
# defaults to the tester's source tree.
prepare() {
  :
}
# ---- end of challenge-specific part ------------------------------------------

tester_bin_var="${prefix}_TESTER_BIN"
# Toolchain, course and tester all come from the devShell (pinned in flake.lock).
if [[ -z "${!tester_bin_var:-}" ]]; then
  exec nix develop "$flake" --command "$0" "$@"
fi
course_dir_var="${prefix}_COURSE_DIR"
tester_src_var="${prefix}_TESTER_SRC"
course_dir="${!course_dir_var}"
course="$course_dir/course-definition.yml"
TESTER_DIR="${!tester_src_var}"

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
    desc=("$course_dir"/stage_descriptions/*-"${2:-$current}".md)
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
  --skip-to)
    target="${2:?usage: ./test.sh --skip-to <slug>}"
    n="$(stages | awk -F'\t' -v s="$target" '$1 == s { print NR }')"
    [[ -n "$n" ]] || { echo "unknown stage '$target' (see ./test.sh --list)" >&2; exit 1; }
    if [[ "$n" == 1 ]]; then
      from="$course_dir/compiled_starters/python/app"
    else
      previous="$(stages | awk -F'\t' -v n="$((n - 1))" 'NR == n { print $1 }')"
      from="$course_dir/solutions/python/$(printf %02d $((n - 1)))-$previous/code/app"
    fi
    rm -rf "$here/app"
    cp -r "$from" "$here/app"
    chmod -R u+w "$here/app"
    echo "$target" > "$stage_file"
    echo "app/ is now the reference solution before $target; .stage is $target."
    echo "When you pass it, compare with $course_dir/solutions/python/$(printf %02d "$n")-$target/diff/"
    exit 0
    ;;
  -*)
    sed -n '2,10s/^# \{0,1\}//p' "$0" >&2
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
  echo "unknown stage '${1:-$current}' (see ./test.sh --list)" >&2
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

# The tester runs from the repo, as CodeCrafters runs it from /app.
cd "$here"
build
prepare

exec env \
  TESTER_DIR="$TESTER_DIR" \
  CODECRAFTERS_REPOSITORY_DIR="$here" \
  CODECRAFTERS_TEST_CASES_JSON="[$json]" \
  CODECRAFTERS_SKIP_ANTI_CHEAT=true \
  "${!tester_bin_var}"
