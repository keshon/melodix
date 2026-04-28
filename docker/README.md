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

Other variables (e.g. `STORAGE_PATH`, `INIT_SLASH_COMMANDS`, `DEVELOPER_ID`, `DISCORD_GUILD_BLACKLIST`, `VOICE_READY_DELAY_MS`, `WS_SILENCE_TIMEOUT`, `DISCORD_UNHEALTHY_MODE`, `DISCORD_UNHEALTHY_GRACE`, `DISCORD_UNHEALTHY_WINDOW`, `PLAYER_TRANSPORT_RECOVERY_MODE`, `PLAYER_TRANSPORT_SOFT_ATTEMPTS`, `COMMAND_TIMEOUT`, `COMMAND_PARALLELISM`) are optional and match the main app config.

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