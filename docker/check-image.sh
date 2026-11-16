#!/usr/bin/env bash
set -euo pipefail
image=${1:?Pass the image to verify}
volume="melodix-smoke-$RANDOM-$RANDOM"
docker volume create "$volume" >/dev/null
cleanup() { docker volume rm "$volume" >/dev/null; }
trap cleanup EXIT

# --help exits before configuration, token use or Discord connection.
docker run --rm --network none "$image" --help
args=(--rm --network none -v "$volume:/usr/project/data")
docker run "${args[@]}" --entrypoint sh "$image" -ec '
    test "$(id -u)" = 10001
    test -r assets/about-banner.webp
    printf persisted > data/container-smoke
    ffmpeg -version >/dev/null
    node --version
    yt-dlp --version
'
docker run "${args[@]}" --entrypoint sh "$image" -ec '
    test "$(cat data/container-smoke)" = persisted
'
echo 'Container runtime and persistent storage passed.'
