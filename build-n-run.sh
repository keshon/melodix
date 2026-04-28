#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

TARGET="${1:-discord}"

case "${TARGET,,}" in
  discord)
    MAIN_PKG="${SCRIPT_DIR}/cmd/discord"
    OUTPUT="${SCRIPT_DIR}/melodix-discord"
    DESC="Discord music bot that allows you to play music from YouTube, SoundCloud and internet radio streams."
    ;;
  cli)
    MAIN_PKG="${SCRIPT_DIR}/cmd/cli"
    OUTPUT="${SCRIPT_DIR}/melodix-cli"
    DESC="CLI music player - same playback engine as the Discord bot."
    ;;
  *)
    echo "Usage: $(basename "$0") [discord|cli]"
    echo "  discord - build and run Discord bot (default)"
    echo "  cli     - build and run CLI player"
    exit 1
    ;;
esac

if [[ ! -f "${MAIN_PKG}/main.go" ]]; then
  echo "ERROR: main.go not found in ${MAIN_PKG}"
  exit 1
fi

echo "[1/3] Gathering build info [${TARGET}]..."

BUILD_DATE="$(date -u +"%Y-%m-%dT%H-%M-%SZ")"
GIT_COMMIT="$(git -C "${SCRIPT_DIR}" rev-parse --short HEAD 2>/dev/null || true)"
if [[ -z "${GIT_COMMIT}" ]]; then
  GIT_COMMIT="none"
fi

# go tool link parses -ldflags similar to shell; keep the description quoted to preserve spaces.
DESC_ESCAPED="${DESC//\'/\'\\\'\'}"
LD_FLAGS="-X github.com/keshon/buildinfo.Version=dev"
LD_FLAGS+=" -X github.com/keshon/buildinfo.Commit=${GIT_COMMIT}"
LD_FLAGS+=" -X github.com/keshon/buildinfo.BuildTime=${BUILD_DATE}"
LD_FLAGS+=" -X github.com/keshon/buildinfo.Project=Melodix"
LD_FLAGS+=" -X 'github.com/keshon/buildinfo.Description=${DESC_ESCAPED}'"

echo "[2/3] Building ${OUTPUT}..."

go build -o "${OUTPUT}" -ldflags "${LD_FLAGS}" "${MAIN_PKG}"

echo "[3/3] Running ${OUTPUT}..."

exec "${OUTPUT}"
