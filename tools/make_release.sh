#!/usr/bin/env bash
# Build the tarballs that carry the course into an airgapped network:
#
#   tools/make_release.sh [OUT_DIR]          (default: dist/)
#
#   framewise-course-<rev>.tar.gz    for the learner: stages, starter, tools, tester. No solutions.
#   framewise-reviewer-<rev>.tar.gz  for the reviewer: the same, plus reference solutions and their
#                                    diffs, fwdetect's source, verify_course.sh, fast_track.sh,
#                                    brief.py and REVIEWING.md.
#
# Each unpacks to build-your-own-framewise/ and framewise-tester/ side by side, the layout
# tools/run_tests.sh expects. The tester comes as a static linux/amd64 binary and as source with its
# Go modules vendored, so it rebuilds offline. With IMAGES=1 the container images the tools use
# are saved in too (images/*.tar; load them with `podman load -i`). With HELM=/path/to/helm that
# binary goes in as bin/helm, for the chart stages (hm1, hm2); take the static linux-amd64 build
# from https://github.com/helm/helm/releases.
#
# Needs: git, go (with the tester's modules in its cache or network), and podman for IMAGES=1.
set -euo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
tester_src="${TESTER_SRC:-$course_dir/../framewise-tester}"
out="$(mkdir -p "${1:-$course_dir/dist}" && cd "${1:-$course_dir/dist}" && pwd)"
rev="$(git -C "$course_dir" rev-parse --short HEAD)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The tester: source (tracked files) with vendored modules, and a static binary.
mkdir -p "$work/framewise-tester"
git -C "$tester_src" archive HEAD | tar -x -C "$work/framewise-tester"
(cd "$work/framewise-tester" && go mod vendor && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o dist/tester ./cmd/tester && cp test.sh dist/)

# The course, as committed.
mkdir -p "$work/full"
git -C "$course_dir" archive HEAD | tar -x -C "$work/full"

learner=(
  README.md LICENSE course-definition.yml stage_descriptions compiled_starters dockerfiles
  tools/run_tests.sh tools/registries.conf
)
reviewer=("${learner[@]}" REVIEWING.md solutions packages starter_templates
  tools/verify_course.sh tools/fast_track.sh tools/brief.py tools/compile.sh)

pack() {
  local name="$1"
  shift
  local stage="$work/$name"
  mkdir -p "$stage/build-your-own-framewise/tools"
  for path in "$@"; do
    [[ -e "$work/full/$path" ]] || continue
    mkdir -p "$stage/build-your-own-framewise/$(dirname "$path")"
    cp -a "$work/full/$path" "$stage/build-your-own-framewise/$path"
  done
  cp -a "$work/framewise-tester" "$stage/framewise-tester"
  if [[ "${IMAGES:-}" == 1 ]]; then
    mkdir -p "$stage/images"
    for image in docker.io/library/postgres:18 ghcr.io/astral-sh/uv:python3.12-bookworm-slim; do
      podman pull -q "$image" >/dev/null
      podman save -o "$stage/images/$(basename "${image//:/-}").tar" "$image"
    done
  fi
  if [[ -n "${HELM:-}" ]]; then
    mkdir -p "$stage/bin"
    cp "$HELM" "$stage/bin/helm"
  fi
  tar -czf "$out/$name-$rev.tar.gz" -C "$stage" .
  echo "$out/$name-$rev.tar.gz ($(du -h "$out/$name-$rev.tar.gz" | cut -f1))"
}

pack framewise-course "${learner[@]}"
pack framewise-reviewer "${reviewer[@]}"
