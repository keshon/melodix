# Docker Deployment

The image is built with the **repository root** as the Docker build context (same layout as local development: `go.mod`, `cmd/`, `internal/`, `pkg/` at the top level).

## Prerequisites

- Docker and Docker Compose installed
- Git (optional; only if you use the clone step in `build-n-deploy.sh`)
- A Discord bot token from the [Discord Developer Portal](https://discord.com/developers/applications)
- **External network:** The Compose file uses a `proxy` network. Create it if it does not exist:

  ```bash
  docker network create proxy
  ```

## Configuration

Copy `docker/.env.example` to `docker/.env` (or `.env` at the repo root) and set at least:

- `DISCORD_TOKEN` — your bot token (required)
- `ALIAS` — container name and image tag (e.g. `melodix`)
- `GIT` / `GIT_URL` — set `GIT=false` to skip cloning; the default build uses the **current checkout** at the repo root

Other variables (e.g. `STORAGE_PATH`, `INIT_SLASH_COMMANDS`, `DEVELOPER_ID`, `DISCORD_GUILD_BLACKLIST`, `VOICE_READY_DELAY_MS`, `MUSIC_PLAYBACK_HISTORY_LIMIT`) are optional and match the main app config.

## Build manually (from repo root)

```bash
docker build -f docker/Dockerfile -t melodix-image .
```

## Deployment

**Option 1 — Build and deploy (script)**  
From the **repository root**:

```bash
./docker/build-n-deploy.sh
```

The script loads `docker/.env` or `.env`, optionally clones into `./src` when `GIT=true`, builds with `-f docker/Dockerfile` from the repo root, and starts Compose.

**Option 2 — Compose only**  
If the image is already built:

```bash
docker compose -f docker/docker-compose.yml up -d
```

Data is persisted in `./data` at the repo root when Compose is run from the root (mounted at `/usr/project/data` in the container).

## Legacy `src/` layout

Older setups expected sources under `./src` inside `docker/`. That is no longer required: the Dockerfile copies the whole tree from the build context. You can still set `GIT=true` to clone into `./src` for other workflows; the Docker build does not depend on `./src` when building from the repo root.
