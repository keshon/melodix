#!/bin/bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DOCKER_COMPOSE_COMMAND="docker compose -f docker/docker-compose.yml up -d"

# Step 1: Load .env (from docker/ directory)
echo "1. Loading env..."
if [ -f docker/.env ]; then
    # shellcheck source=/dev/null
    source docker/.env
elif [ -f .env ]; then
    # shellcheck source=/dev/null
    source .env
else
    echo "ERROR: docker/.env or .env not found!"
    exit 1
fi

# Step 2: Optional clone into legacy ./src (not required when building from repo root)
if [ "${GIT:-}" != "false" ]; then
    echo "2. Cloning repository into ./src (set GIT=false to skip)..."
    rm -rf ./src
    git clone "$GIT_URL" src
fi

# Step 3: Bring down running containers
echo "3. Stopping containers..."
docker compose -f docker/docker-compose.yml down --remove-orphans

# Step 4: Remove old image(s) related to ALIAS
echo "4. Removing old images..."
OLD_IMAGES=$(docker images --filter=reference="${ALIAS}-image" -q)

if [ -n "$OLD_IMAGES" ]; then
    docker rmi -f $OLD_IMAGES || true
fi

# Step 5: Build image (context = repo root)
echo "5. Building new image..."
DOCKER_BUILDKIT=1 docker build -f docker/Dockerfile -t "${ALIAS}-image" .

# Step 6: Start up containers
echo "6. Starting containers..."
eval "$DOCKER_COMPOSE_COMMAND"

# Step 7: Prune unused Docker junk
echo "7. Cleaning up dangling Docker artifacts..."
docker image prune -f
