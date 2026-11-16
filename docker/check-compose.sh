#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

# All checks are offline: config renders without creating containers or networks.
compose=(docker compose)
if [ -n "${COMPOSE_BIN:-}" ]; then compose=("$COMPOSE_BIN"); fi
workspace=$(mktemp -d "${TMPDIR:-/tmp}/bot-compose.XXXXXXXX")
cleanup() {
    case "$workspace" in
        "${TMPDIR:-/tmp}"/bot-compose.*) rm -rf -- "$workspace" ;;
        *) echo 'Unexpected fixture path; retained.' >&2; return 1 ;;
    esac
}
trap cleanup EXIT
printf 'DISCORD_TOKEN=offline-compose-fixture\nLOG_LEVEL=debug\n' > "$workspace/bot.env"
cp compose.yaml "$workspace/compose.yaml"
# Resolve env_file inside the fixture, leaving any operator .env untouched.
cp "$workspace/bot.env" "$workspace/.env"
export DISCORD_TOKEN=offline-compose-fixture
unset DATA_DIR MEDIA_DIR IMAGE_TAG COMPOSE_PROJECT_NAME COMPOSE_FILE

"${compose[@]}" --env-file "$workspace/bot.env" -f "$workspace/compose.yaml" config --format json > "$workspace/pull.json"
cp compose.build.yaml "$workspace/compose.build.yaml"
"${compose[@]}" --env-file "$workspace/bot.env" -f "$workspace/compose.yaml" -f "$workspace/compose.build.yaml" config --format json > "$workspace/build.json"
python3 - "$workspace" <<'PY'
import json
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
pull = json.loads((root / "pull.json").read_text())
build = json.loads((root / "build.json").read_text())
a, b = pull["services"]["app"], build["services"]["app"]
assert a["image"] == "keshon/melodix:latest"
assert "build" not in a
assert b["image"] == "melodix-local:dev" and b["pull_policy"] == "build"
assert "ports" not in a and "labels" not in a
assert a["environment"]["DISCORD_TOKEN"] == "offline-compose-fixture"
assert a["environment"]["LOG_LEVEL"] == "debug"
assert pull["name"] == build["name"] == "melodix"
for field in ["volumes", "environment", "restart", "logging", "stop_grace_period", "init"]:
    assert a[field] == b[field], field
for filename in ["traefik.json", "build-traefik.json"]:
    path = root / filename
    if path.exists():
        proxied = json.loads(path.read_text())["services"]["app"]
        assert proxied["volumes"] == a["volumes"]
        assert "ports" not in proxied
        assert proxied["labels"]["traefik.enable"] == "true"
        assert proxied["environment"]["SHORTLINK_ADDR"] == ":8787"
print("Compose pull, build and optional routing modes passed.")
PY
