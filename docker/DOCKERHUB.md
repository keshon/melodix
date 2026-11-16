# Melodix

A self-hosted Discord music bot for YouTube, SoundCloud and internet radio,
with queues, playlists and playback history. The image includes FFmpeg,
yt-dlp and Node.js.

[Source code](https://github.com/keshon/melodix) ·
[Deployment guide](https://github.com/keshon/melodix/blob/main/docker/README.md) ·
[Bot setup and configuration](https://github.com/keshon/melodix/blob/main/docs/running.md)

## Quick start

Install Docker Engine with Compose 2.24+ and create and invite a Discord bot
using the setup guide above. Download the deployment files:

```sh
mkdir melodix && cd melodix
curl -fsSLO https://raw.githubusercontent.com/keshon/melodix/main/docker/compose.yaml
curl -fsSL https://raw.githubusercontent.com/keshon/melodix/main/docker/.env.example -o .env
```

Edit `.env`: set `DISCORD_TOKEN` to your bot token and `IMAGE_TAG=edge`.
Then start the bot:

```sh
docker compose up -d
docker compose logs --tail=100 app
```

Public images do not require a Docker Hub login. Keep your Discord token private.
The deployment needs no host ports, external Docker network or reverse proxy.

## Tags and platforms

- `edge`: builds from GitHub `main`.
- `latest`: the most recent stable release published by the container workflow.
- `v*`: exact Git release tags built by that workflow.
- `sha-<commit>`: a full commit SHA identifying a published build.

Images support Linux `amd64` and `arm64`. Both architectures are built and
smoke-tested before combined public tags are published.

The image runs as root (UID/GID `0:0`) inside the container. Existing local
bind-mounted data needs no UID/GID ownership setup. The bot can modify files
in writable mounts as root; new files are normally root-owned on standard
Linux Docker. Mount only directories intended for the bot. Rootless Docker
and host security policies can still restrict access.

## Storage and updates

Compose stores application state in a named volume at `/usr/project/data`.
Keep the Compose project name unchanged to reuse that storage across updates.
See the deployment guide for host directories, backups, source builds and rollback instructions.

To update, select the intended `IMAGE_TAG` in `.env`, then run:

```sh
docker compose pull
docker compose up -d
docker compose logs --tail=100 app
```

Replacement briefly restarts the bot. Verify playback and persisted settings
in Discord afterward. Avoid `docker compose down -v` when retaining stored state.
