#!/usr/bin/env bash
set -e
app_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
app_binary="$app_root/melodix-$1"
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) app_binary="$app_binary.exe" ;;
esac
if [[ -f "$app_root/go.mod" ]]; then
    (cd -- "$app_root" && go build -trimpath -o "$app_binary" "./cmd/$1")
elif [[ ! -f "$app_binary" ]]; then
    printf 'ERROR: %s is missing. Extract the complete release archive.\n' "$app_binary" >&2
    exit 1
fi
