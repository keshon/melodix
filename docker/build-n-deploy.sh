#!/usr/bin/env bash
#
# build-n-deploy.sh — rebuild the Docker image and (re)start the stack.
#
# Flow: load .env → fetch source → stop stack → drop old image → build → start → prune.
# Runs from its own directory, so it works regardless of where you call it from.
#
# Config (via .env or environment):
#   ALIAS     container + image name          (required)
#   GIT       "true" to clone, "false" to use existing ./src   (default: true)
#   GIT_URL   repository to clone             (required when GIT=true)

set -euo pipefail

# Always operate from the directory this script lives in.
cd "$(dirname "${BASH_SOURCE[0]}")"

# --- Tunables (override via environment if needed) ---
ENV_FILE="${ENV_FILE:-.env}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
SRC_DIR="${SRC_DIR:-./src}"

# --- Pretty output (colors only when writing to a terminal) ---
if [ -t 1 ]; then
    BOLD=$'\e[1m'; DIM=$'\e[2m'; RED=$'\e[31m'; GREEN=$'\e[32m'; RESET=$'\e[0m'
else
    BOLD=''; DIM=''; RED=''; GREEN=''; RESET=''
fi

step_no=0
step() { step_no=$((step_no + 1)); printf '%s[%d]%s %s\n' "$BOLD" "$step_no" "$RESET" "$1"; }
info() { printf '    %s%s%s\n' "$DIM" "$1" "$RESET"; }
die()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$1" >&2; exit 1; }

start_time=$SECONDS

# --- 1. Load .env -----------------------------------------------------------
step "Loading ${ENV_FILE}"
[ -f "$ENV_FILE" ] || die "${ENV_FILE} not found (copy .env.example to ${ENV_FILE})"
# shellcheck disable=SC1090  # runtime-provided path
set -a; source "$ENV_FILE"; set +a
[ -n "${ALIAS:-}" ] || die "ALIAS is not set in ${ENV_FILE}"

IMAGE="${ALIAS}-image"

# --- 2. Fetch source --------------------------------------------------------
if [ "${GIT:-true}" != "false" ]; then
    [ -n "${GIT_URL:-}" ] || die "GIT_URL is not set (or set GIT=false to use existing ${SRC_DIR})"
    step "Cloning ${GIT_URL}"
    rm -rf "$SRC_DIR"
    git clone "$GIT_URL" "$SRC_DIR"
else
    step "Using existing source in ${SRC_DIR}"
    [ -d "$SRC_DIR" ] || die "${SRC_DIR} not found (set GIT=true to clone it)"
fi

# --- 3. Stop the running stack ----------------------------------------------
step "Stopping containers"
docker compose -f "$COMPOSE_FILE" down --remove-orphans

# --- 4. Remove the old image ------------------------------------------------
step "Removing old image (${IMAGE})"
old_images=$(docker images --filter=reference="$IMAGE" -q)
if [ -n "$old_images" ]; then
    # shellcheck disable=SC2086  # intentional split: pass each image id as a separate arg
    docker rmi -f $old_images || true
else
    info "none found"
fi

# --- 5. Build the new image -------------------------------------------------
step "Building ${IMAGE}"
DOCKER_BUILDKIT=1 docker build -t "$IMAGE" .

# --- 6. Start the stack -----------------------------------------------------
step "Starting containers"
docker compose -f "$COMPOSE_FILE" up -d

# --- 7. Prune dangling artifacts --------------------------------------------
step "Pruning dangling images"
docker image prune -f

printf '%sdone%s in %ds — %s is up.\n' "$GREEN" "$RESET" "$((SECONDS - start_time))" "$ALIAS"
