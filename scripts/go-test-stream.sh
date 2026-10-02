#!/usr/bin/env bash
# Stream readable Go test output across packages without serializing execution.
set -euo pipefail

go test -json "$@" | jq --unbuffered -j 'select(.Output != null) | .Output'
