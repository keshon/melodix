# Docker deployment

Run commands from this `docker/` directory. The image builds from a source
checkout in `./src`, includes FFmpeg, Node and yt-dlp, and runs the Discord bot.
Create and invite the bot as described in [Running Melodix](../docs/running.md#discord-bot).

## Prerequisites

Install Docker with Compose. The build script also needs Git when cloning source.
This Compose file attaches to an external network named `proxy`; create it once
if your deployment does not already have it:

```sh
docker network create proxy
```

The network name does not configure an HTTP proxy. See the
[proxy instructions](../docs/running.md#proxies) if one is needed.

## Configuration

Copy `.env.example` to `.env` in this directory. Set `DISCORD_TOKEN` and retain
`INIT_SLASH_COMMANDS=true` for initial registration. Set `ALIAS` to the container
and image name. With `GIT=true`, the script clones `GIT_URL` into `./src`;
with `GIT=false`, supply that directory yourself.

The shared [configuration reference](../docs/running.md#configuration) owns
application variables and code defaults. Container-specific values are:

- `STORAGE_PATH=/usr/project/data/store` and `CACHE_DIR=/usr/project/data/cache`.
  Both are under the `./data:/usr/project/data` bind mount. Writes outside that
  mount do not survive replacing the container.
- `BUFFER_AHEAD_MS=10000` in the supplied Docker example and Compose fallback.
  This overrides the application's 30000 ms default.

Compose forwards variables explicitly. A setting in `.env` has no effect if
`docker-compose.yml` does not forward it. The current `COMMAND_TIMEOUT` entry is
unused by the application. Recovery settings have the same meaning as in the
shared reference; a container/process restart clears the pending queue.

## Deployment

For a fresh deployment with the directory above prepared:

```sh
./build-n-deploy.sh
```

The script sources `.env`, replaces `./src` with a fresh clone when `GIT=true`,
stops the Compose stack, removes its old image, builds, starts the new container
and prunes dangling images. Do not keep unique work in `./src` in Git mode.
It stops the service before building; a failed build can therefore leave it down.

To start an already built image, or recreate it after environment changes:

```sh
docker compose -f docker-compose.yml up -d
```

## Verify the container

```sh
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs --tail=100 app
```

Check `track_cache_enabled` or `track_cache_disabled` for the effective cache,
read-ahead and bitrate settings. Then join a voice channel, play a track and
check `/queue` and `/stop`. A running container alone does not establish audio
or command registration. Redact tokens/private URLs before sharing logs.

## Update and recover

Before rebuilding an existing deployment, stop it cleanly and back up the whole
mounted `./data` directory and edited `.env`. Keep the previous source revision
or release and a separately tagged copy of its image: the build script removes
the ordinary image tag before building. Do not rely on that tag as rollback.

For a pinned source build, populate `./src` with the chosen revision and set
`GIT=false`; Git mode clones the repository's default branch. After rebuilding,
verify settings and playback before discarding backups. If it fails, stop the
new container and restore the previous image/source with its pre-update data
backup. The exact recovery commands depend on the retained image/source; test
them on a separate stack before using this script for an unattended update.

These instructions describe the supplied scripts. They have not been exercised
against a live Docker host as part of the documentation migration.
