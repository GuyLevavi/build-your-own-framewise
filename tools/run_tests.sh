#!/usr/bin/env bash
# Run the Framewise tester against a solution directory, the way CodeCrafters would.
#
#   tools/run_tests.sh CODE_DIR [STAGE_SLUG] [--only]
#
# Runs every stage from the first up to STAGE_SLUG (default: all), or just STAGE_SLUG with --only.
# By default the program runs in a container built from dockerfiles/, with CODE_DIR mounted at
# /app, so nothing but podman (or docker) is needed on the host. With FRAMEWISE_RUNNER=native it
# runs on the host with uv instead (the twentyx pod, or any machine with uv and Python 3.12).
#
# Environment:
#   FRAMEWISE_RUNNER  container (default) or native
#   FRAMEWISE_TEST_DATABASE_URL  an existing Postgres for the database stages; the tester
#                creates and drops its own databases there. Unset: the tester starts its own.
#   TESTER_DIR   directory holding `tester` and `test.sh` (default: tester/dist in this repo,
#                built with `make build` there if missing)
#   CPUS         CPU limit for the container (default: all CPUs, at most 4; the pf stages
#                measure speed-ups across processes)
#   FRAMEWISE_CI_IMAGE, FRAMEWISE_BASE_IMAGE, FRAMEWISE_POSTGRES_IMAGE
#                images the delivery stages (ci1 on) use; defaults are in the airgap tarball
#   (set `debug: true` in CODE_DIR/codecrafters.yml for verbose tester logs)
#
# The delivery stages (ci1, im1, im2, hm1) build images, run pods and render the chart
# with podman, helm and git on this machine, as you would on a workstation. In container mode
# they run after the others, with the tester on the host; they never run your program directly.
set -euo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
code_dir="$(cd "${1:?usage: tools/run_tests.sh CODE_DIR [STAGE_SLUG] [--only]}" && pwd)"
until="${2:-}"
only="${3:-}"
export CONTAINERS_REGISTRIES_CONF="${CONTAINERS_REGISTRIES_CONF:-$course_dir/tools/registries.conf}"

tester_dir="${TESTER_DIR:-$course_dir/tester/dist}"
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

host_stages=" ci1 im1 im2 hm1 "
json="["
host_json="["
core_json="["
found=""
for i in "${!stages[@]}"; do
  slug="${stages[$i]%%|*}"
  name="${stages[$i]#*|}"
  if [[ "$only" == "--only" && "$slug" != "$until" ]]; then
    continue
  fi
  entry="{\"slug\":\"$slug\",\"tester_log_prefix\":\"stage-$((i + 1))\",\"title\":\"Stage $((i + 1)): $name\"}"
  [[ "$json" == "[" ]] || json+=","
  json+="$entry"
  if [[ "$host_stages" == *" $slug "* ]]; then
    [[ "$host_json" == "[" ]] || host_json+=","
    host_json+="$entry"
  else
    [[ "$core_json" == "[" ]] || core_json+=","
    core_json+="$entry"
  fi
  if [[ "$slug" == "$until" ]]; then
    found=1
    break
  fi
done
json+="]"
host_json+="]"
core_json+="]"
if [[ -n "$until" && -z "$found" ]]; then
  echo "Unknown stage slug: $until" >&2
  exit 2
fi

# The reference solutions link models/ to the starter's (tools/compile.sh): run them from a copy
# that has the files, as a learner's repository does, so containers and image builds see them.
if [[ -L "$code_dir/models" ]]; then
  staged="$(mktemp -d)"
  trap 'rm -rf "$staged"' EXIT
  cp -a "$code_dir/." "$staged/"
  rm "$staged/models"
  cp -rL "$code_dir/models" "$staged/models"
  code_dir="$staged"
fi

if [[ "${FRAMEWISE_RUNNER:-container}" == native ]]; then
  # One virtualenv per dependency set, outside CODE_DIR, so solutions stay clean.
  deps_hash="$(cat "$code_dir/pyproject.toml" "$code_dir/uv.lock" | sha256sum | cut -c1-12)"
  export UV_PROJECT_ENVIRONMENT="${XDG_CACHE_HOME:-$HOME/.cache}/framewise/venv-$deps_hash"
  cd "$code_dir"
  env \
    TESTER_DIR="$tester_dir" \
    PYTHONDONTWRITEBYTECODE=1 \
    CODECRAFTERS_REPOSITORY_DIR="$code_dir" \
    CODECRAFTERS_TEST_CASES_JSON="$json" \
    CODECRAFTERS_RANDOM_SEED="${CODECRAFTERS_RANDOM_SEED:-}" \
    "$tester_dir/tester"
  exit
fi

# The delivery stages, with the tester on this machine.
run_host_stages() {
  [[ "$host_json" == "[]" ]] && return 0
  cd "$code_dir"
  env \
    TESTER_DIR="$tester_dir" \
    CODECRAFTERS_REPOSITORY_DIR="$code_dir" \
    CODECRAFTERS_TEST_CASES_JSON="$host_json" \
    CODECRAFTERS_RANDOM_SEED="${CODECRAFTERS_RANDOM_SEED:-}" \
    "$tester_dir/tester"
}

if [[ "$core_json" == "[]" ]]; then
  run_host_stages
  exit
fi

engine="$(command -v podman || command -v docker)"

# The image holds only the dependencies, keyed by the dependency files, so code changes need no rebuild.
dockerfile="$(ls "$course_dir"/dockerfiles/python-*.Dockerfile | sort -V | tail -1)"
deps_hash="$(cat "$dockerfile" "$code_dir/pyproject.toml" "$code_dir/uv.lock" | sha256sum | cut -c1-12)"
image="framewise-deps:$deps_hash"
if ! "$engine" image exists "$image" 2>/dev/null; then
  context="$(mktemp -d)"
  cp -r "$code_dir/pyproject.toml" "$code_dir/uv.lock" "$context/"
  [[ -f "$code_dir/.python-version" ]] && cp "$code_dir/.python-version" "$context/"
  echo "Building $image (dependencies only; cached until pyproject.toml or uv.lock change) ..."
  "$engine" build -q -t "$image" -f "$dockerfile" "$context" >/dev/null
  rm -rf "$context"
fi

"$engine" run --rm \
  --cpus "${CPUS:-$(( $(nproc) < 4 ? $(nproc) : 4 ))}" --memory 4g \
  -v "$code_dir:/app:z" \
  -v "$tester_dir:/var/opt/tester:ro,z" \
  -e TESTER_DIR=/var/opt/tester \
  -e PYTHONDONTWRITEBYTECODE=1 \
  -e CODECRAFTERS_REPOSITORY_DIR=/app \
  -e CODECRAFTERS_TEST_CASES_JSON="$core_json" \
  -e CODECRAFTERS_RANDOM_SEED="${CODECRAFTERS_RANDOM_SEED:-}" \
  -e FRAMEWISE_TEST_DATABASE_URL="${FRAMEWISE_TEST_DATABASE_URL:-}" \
  --network host \
  -w /app \
  "$image" /var/opt/tester/test.sh || exit
run_host_stages
