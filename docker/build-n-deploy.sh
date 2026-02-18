#!/bin/bash

set -euo pipefail

DOCKER_COMPOSE_COMMAND="docker compose -f docker-compose.yml up -d"

# Step 1: Load .env
echo "1. Loading env..."
if [ -f .env ]; then
    source .env
else
    echo "ERROR: .env file not found!"
    exit 1
fi

# Step 2: Pull or verify source code
if [ "${GIT:-}" != "false" ]; then
    echo "2. Cloning repository..."
    rm -rf ./src
    git clone "$GIT_URL" src
else
    if [ ! -d "./src" ]; then
        echo "ERROR: src directory not found!"
        exit 1
    fi
fi

# Step 3: Bring down running containers
echo "3. Stopping containers..."
docker compose down --remove-orphans

# Step 4: Remove old image(s) related to ALIAS
echo "4. Removing old images..."
OLD_IMAGES=$(docker images --filter=reference="${ALIAS}-image" -q)

if [ -n "$OLD_IMAGES" ]; then
    docker rmi -f $OLD_IMAGES || true
fi

# Step 5: Build image
echo "5. Building new image..."
DOCKER_BUILDKIT=1 docker build -t "${ALIAS}-image" .

# Step 6: Start up containers
echo "6. Starting containers..."
eval "$DOCKER_COMPOSE_COMMAND"

# Step 7: Prune unused Docker junk
echo "7. Cleaning up dangling Docker artifacts..."
docker image prune -f
