#!/bin/sh
#
# Use this script to run your program LOCALLY.
#
# This runs after .codecrafters/compile.sh
#
# Learn more: https://codecrafters.io/program-interface

set -e # Exit on failure

exec uv run --quiet -m app.main "$@"
