#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
if [[ ! -f node_modules/vite/bin/vite.js ]]; then npm ci; fi
exec npm run dev -- --open "$@"
