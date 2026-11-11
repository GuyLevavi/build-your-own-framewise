#!/bin/sh
#
# Use this script to run your program LOCALLY.
#
# This runs after .codecrafters/compile.sh
#
# Learn more: https://codecrafters.io/program-interface

set -e # Exit on failure

# In the nix dev shell (./test.sh python), Python and the dependencies come from nixpkgs:
# the opencv wheel uv would install doesn't run on NixOS.
if [ -n "${FRAMEWISE_NIX_PYTHON:-}" ]; then
  exec python -m app.main "$@"
fi
exec uv run --quiet -m app.main "$@"
