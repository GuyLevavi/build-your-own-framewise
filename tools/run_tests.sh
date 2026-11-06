#!/usr/bin/env bash
# Run the Framewise tester against a solution directory, the way CodeCrafters would.
#
#   tools/run_tests.sh CODE_DIR [STAGE_SLUG] [--only]
#
# Runs every stage from the first up to STAGE_SLUG (default: all), or just STAGE_SLUG with --only.
# The program runs in a container built from dockerfiles/, with CODE_DIR mounted at /app, so
# nothing but podman (or docker) is needed on the host.
#
# Environment:
#   TESTER_DIR   directory holding `tester` and `test.sh` (default: ../framewise-tester/dist,
#                built with `make build` there if missing)
#   CPUS         CPU limit for the container (default 0.5, as on CodeCrafters)
#   (set `debug: true` in CODE_DIR/codecrafters.yml for verbose tester logs)
set -euo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
code_dir="$(cd "${1:?usage: tools/run_tests.sh CODE_DIR [STAGE_SLUG] [--only]}" && pwd)"
until="${2:-}"
only="${3:-}"
engine="$(command -v podman || command -v docker)"
export CONTAINERS_REGISTRIES_CONF="${CONTAINERS_REGISTRIES_CONF:-$course_dir/tools/registries.conf}"

tester_dir="${TESTER_DIR:-$course_dir/../framewise-tester/dist}"
if [[ ! -x "$tester_dir/tester" ]]; then
  echo "Building the tester in $(dirname "$tester_dir") ..."
  if command -v go >/dev/null; then
    make -C "$(dirname "$tester_dir")" build
  else
    nix shell nixpkgs#go nixpkgs#gnumake -c make -C "$(dirname "$tester_dir")" build
  fi
fi
tester_dir="$(cd "$tester_dir" && pwd)"

# Stage list from course-definition.yml, in order: "slug|name" per line.
mapfile -t stages < <(awk '
  /^stages:/ { in_stages = 1; next }
  in_stages && /^  - slug:/ { gsub(/.*slug: *"|".*/, ""); slug = $0; next }
  in_stages && /^    name:/ && slug != "" { gsub(/.*name: *"|".*/, ""); print slug "|" $0; slug = "" }
' "$course_dir/course-definition.yml")

json="["
found=""
for i in "${!stages[@]}"; do
  slug="${stages[$i]%%|*}"
  name="${stages[$i]#*|}"
  if [[ "$only" == "--only" && "$slug" != "$until" ]]; then
    continue
  fi
  [[ "$json" == "[" ]] || json+=","
  json+="{\"slug\":\"$slug\",\"tester_log_prefix\":\"stage-$((i + 1))\",\"title\":\"Stage $((i + 1)): $name\"}"
  if [[ "$slug" == "$until" ]]; then
    found=1
    break
  fi
done
json+="]"
if [[ -n "$until" && -z "$found" ]]; then
  echo "Unknown stage slug: $until" >&2
  exit 2
fi

# The image holds only the dependencies, keyed by the dependency files, so code changes need no rebuild.
dockerfile="$(ls "$course_dir"/dockerfiles/python-*.Dockerfile | sort -V | tail -1)"
deps_hash="$(cat "$dockerfile" "$code_dir/pyproject.toml" "$code_dir/uv.lock" | sha256sum | cut -c1-12)"
image="framewise-deps:$deps_hash"
if ! "$engine" image exists "$image" 2>/dev/null; then
  context="$(mktemp -d)"
  cp "$code_dir/pyproject.toml" "$code_dir/uv.lock" "$context/"
  [[ -f "$code_dir/.python-version" ]] && cp "$code_dir/.python-version" "$context/"
  echo "Building $image (dependencies only; cached until pyproject.toml or uv.lock change) ..."
  "$engine" build -q -t "$image" -f "$dockerfile" "$context" >/dev/null
  rm -rf "$context"
fi

exec "$engine" run --rm \
  --cpus "${CPUS:-0.5}" --memory 2g \
  -v "$code_dir:/app:z" \
  -v "$tester_dir:/var/opt/tester:ro,z" \
  -e TESTER_DIR=/var/opt/tester \
  -e PYTHONDONTWRITEBYTECODE=1 \
  -e CODECRAFTERS_REPOSITORY_DIR=/app \
  -e CODECRAFTERS_TEST_CASES_JSON="$json" \
  -e CODECRAFTERS_RANDOM_SEED="${CODECRAFTERS_RANDOM_SEED:-}" \
  -w /app \
  "$image" /var/opt/tester/test.sh
