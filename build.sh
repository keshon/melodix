#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'ERROR: Go was not found. Install the Go version required by go.mod.' >&2
    exit 1
fi
exec go run ./cmd/build "$@"
