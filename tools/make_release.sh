#!/usr/bin/env bash
# Build the tarballs that carry the course into an airgapped network:
#
#   tools/make_release.sh [OUT_DIR]          (default: dist/)
#
#   framewise-course-<rev>.tar.gz    for the learner: stages, starter, tools, tester. No solutions.
#   framewise-reviewer-<rev>.tar.gz  for the reviewer: the same, plus reference solutions and their
#                                    diffs, verify_course.sh, fast_track.sh,
#                                    brief.py and REVIEWING.md.
#
# Each unpacks to build-your-own-framewise/, with the tester in its tester/ as tools/run_tests.sh
# expects. The tester comes as a static linux/amd64 binary (tester/dist) and as source with its
# Go modules vendored, so it rebuilds offline. Everything the course needs on the other side goes
# in by default:
#   images/      every image in tools/images.env, saved (images/load.sh loads them into podman);
#                IMAGES=0 leaves them out
#   bin/helm     for the chart stages (hm1): $HELM, else the flake's static `.#helm` when nix
#                is there, else a static helm on PATH; HELM= (empty) leaves it out. It must be a
#                static linux-amd64 build, like `.#helm` or https://github.com/helm/helm/releases.
#
# Needs: git, go (with the tester's modules in its cache or network), podman (or docker) to
# pull and save the images. `nix develop` has go and helm; podman comes from the system.
set -euo pipefail

course_dir="$(cd "$(dirname "$0")/.." && pwd)"
out="$(mkdir -p "${1:-$course_dir/dist}" && cd "${1:-$course_dir/dist}" && pwd)"
rev="$(git -C "$course_dir" rev-parse --short HEAD)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if [[ -n "${HELM+set}" ]]; then
  helm="$HELM"
elif command -v nix >/dev/null; then
  helm="$(nix build --no-link --print-out-paths "$course_dir#helm")/bin/helm"
else
  helm="$(command -v helm || true)"
fi
if [[ -n "$helm" ]] && ! file -L "$helm" | grep -q 'statically linked'; then
  echo "$helm is not statically linked, so it won't run on the pod; set HELM to a static build (or HELM= to leave it out)" >&2
  exit 1
fi
mapfile -t images < <(sed -n 's/^[A-Z_]*=\([^ ]*\).*/\1/p' "$course_dir/tools/images.env" | sort -u)
if [[ "${IMAGES:-1}" == 1 ]]; then
  engine="$(command -v podman || command -v docker)" || { echo "need podman or docker to save the images (or IMAGES=0)" >&2; exit 1; }
  mkdir -p "$work/images"
  for image in "${images[@]}"; do
    "$engine" pull -q "$image" >/dev/null
    "$engine" save -o "$work/images/$(basename "${image//:/-}").tar" "$image"
  done
  cat >"$work/images/load.sh" <<'LOAD'
#!/bin/sh
# Load the course's images (tools/images.env) into podman.
set -e
cd "$(dirname "$0")"
for f in *.tar; do podman load -i "$f"; done
LOAD
  chmod +x "$work/images/load.sh"
fi

# The course, as committed.
mkdir -p "$work/full"
git -C "$course_dir" archive HEAD | tar -x -C "$work/full"

# The tester: source with vendored modules, and a static binary.
(cd "$work/full/tester" && go mod vendor && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o dist/tester ./cmd/tester && cp test.sh dist/)

learner=(
  README.md LICENSE course-definition.yml stage_descriptions compiled_starters dockerfiles tester
  tools/run_tests.sh tools/registries.conf tools/images.env
)
reviewer=("${learner[@]}" REVIEWING.md solutions starter_templates
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
  [[ ! -d "$work/images" ]] || cp -a "$work/images" "$stage/images"
  if [[ -n "$helm" ]]; then
    mkdir -p "$stage/bin"
    cp -L "$helm" "$stage/bin/helm"
    chmod u+w "$stage/bin/helm"
  fi
  tar -czf "$out/$name-$rev.tar.gz" -C "$stage" .
  echo "$out/$name-$rev.tar.gz ($(du -h "$out/$name-$rev.tar.gz" | cut -f1))"
}

pack framewise-course "${learner[@]}"
pack framewise-reviewer "${reviewer[@]}"
