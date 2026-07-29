# Docker Deployment

The deployment uses Docker Compose. The build expects the project source either to be cloned into `./src` by the script or to be present in `./src` when building locally.

## Prerequisites

- Docker and Docker Compose installed
- Git (if using the script to clone the repo)
- A Discord bot token from the [Discord Developer Portal](https://discord.com/developers/applications)
- **External network:** The Compose file uses a `proxy` network. Create it if it does not exist:

  ```bash
  docker network create proxy
  ```

## Configuration

Copy `.env.example` to `.env` in this directory and set at least:

- `DISCORD_TOKEN` — your bot token (required)
- `ALIAS` — container name and image tag (e.g. `melodix`)
- `GIT` / `GIT_URL` — set `GIT=true` to clone the repo into `./src`; set `GIT=false` to use an existing `./src` directory

Other variables (e.g. `STORAGE_PATH`, `INIT_SLASH_COMMANDS`, `DEVELOPER_ID`, `DISCORD_GUILD_BLACKLIST`, `VOICE_READY_DELAY_MS`, `WS_SILENCE_TIMEOUT`, `DISCORD_UNHEALTHY_MODE`, `DISCORD_UNHEALTHY_GRACE`, `DISCORD_UNHEALTHY_WINDOW`, `PLAYER_TRANSPORT_RECOVERY_MODE`, `PLAYER_TRANSPORT_SOFT_ATTEMPTS`, `CACHE_ENABLED`, `CACHE_DIR`, `CACHE_MAX_BYTES`, `CACHE_PERSISTENT`, `BUFFER_AHEAD_MS`, `COMMAND_TIMEOUT`, `COMMAND_PARALLELISM`) are optional and match the main app config.

**Every variable the app reads must be listed in `docker-compose.yml`** — the service passes them through one by one, so a setting present in `.env` but missing from the compose file silently falls back to its built-in default. Keep the two in step when adding config.

Paths like `STORAGE_PATH` and `CACHE_DIR` must live under `/usr/project/data`, which is the mounted volume; anything written elsewhere is lost when the container is recreated.

At startup the bot logs either `track_cache_enabled` (with the directory and caps) or `track_cache_disabled`, which is the quickest way to confirm the setting actually reached the process.

Notes on recovery modes:

- `DISCORD_UNHEALTHY_MODE=restart-session` restarts the Discord gateway session (players/queues stay in-memory); voice sinks are invalidated so playback can re-join quickly.
- `DISCORD_UNHEALTHY_MODE=restart-voice` only drops voice connections (no gateway restart), so players re-join VC on the next sink acquisition.
- `PLAYER_TRANSPORT_RECOVERY_MODE=soft` tries stream reopen first (no voice reconnect), then falls back to a voice reconnect if transport keeps failing.

## Deployment

**Option 1 — Build and deploy (recommended)**  
From this directory (`docker/`), run:

```bash
./build-n-deploy.sh
```

This loads `.env`, clones the repo into `./src` (or uses existing `./src`), builds the image, and starts the container.

**Option 2 — Compose only**  
If the image is already built:

```bash
docker compose -f docker-compose.yml up -d
```

Data is persisted in `./data` (mounted at `/usr/project/data` in the container).