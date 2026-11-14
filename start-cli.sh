#!/usr/bin/env bash
set -e
source "$(dirname -- "${BASH_SOURCE[0]}")/scripts/ensure-build.sh" cli
cd -- "$app_root"
exec "$app_binary" "$@"
