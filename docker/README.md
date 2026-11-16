# Docker deployment

The public Docker Hub overview is maintained in [DOCKERHUB.md](DOCKERHUB.md).
The `Docker Hub overview` workflow syncs it on changes to that file or the
workflow on `main`, using `DOCKERHUB_TOKEN`. It can also run manually from
`main`. The short description is configured in that workflow. Description
updates require a token with Read, Write & Delete repository permission;
Docker Hub groups repository management under that permission. These syncs
do not build images.

Melodix supports two image sources: a published Docker Hub release and a local
build of this checkout. Both use the same configuration, Compose project and
persistent storage. Docker Engine with Compose 2.24+ is required; source builds
also need Git. Create and invite a Discord bot using the
[running guide](../docs/running.md).

## Run a published image

Download `compose.yaml` and `.env.example` from this directory into a deployment
folder, or use this directory in a checkout. Run the following commands from
that folder:

```sh
cp .env.example .env
# Edit .env and set DISCORD_TOKEN.
docker compose up -d
docker compose logs --tail=100 app
```

The image is [`keshon/melodix`](https://hub.docker.com/r/keshon/melodix). Public
images can be pulled without a Docker Hub login. No Go compiler, Git checkout,
external Docker network or reverse proxy is needed for the base deployment.
The example enables initial slash-command registration.

`IMAGE_TAG=latest` selects the most recently published stable release.
`IMAGE_TAG=edge` selects a build from `main`. Use an exact Git release tag, such
as `v2026.11.10.1`, after that tag has a container build, to select a particular
release.

## Build this checkout

Clone the repository and select the intended revision. From its `docker/`
directory:

```sh
cp .env.example .env
# Edit .env and set DISCORD_TOKEN.
docker compose -f compose.yaml -f compose.build.yaml build
docker compose -f compose.yaml -f compose.build.yaml up -d --no-build
```

The Dockerfile builds from the repository root. Local builds use
`melodix-local:dev`; published images keep their separate Docker Hub tags.
Build failure leaves the running container in place. Use the same `-f` arguments
for subsequent source-build operations; plain `docker compose up` selects the
published-image configuration again.

The images run as root (UID/GID `0:0`) inside the container. Existing local
bind-mounted data needs no UID/GID ownership setup. The bot can modify files
in writable mounts as root, and new files are normally root-owned on standard
Linux Docker. Mount only directories intended for the bot. Rootless Docker,
user namespace remapping and host security policies can still restrict access.

Both Linux amd64 and arm64 images are built and smoke-tested on native runners.
Local builds target the builder's native architecture.

## Configuration and storage

Compose reads `.env` and passes its settings through `env_file`. The
[configuration reference](../docs/running.md#configuration) lists all application
settings; copy the ones you need. Do not source `.env` as shell code.
Single-quote values containing `$` to keep them literal. The application uses
its defaults for settings omitted from the Docker example.

The default project name is `melodix`. Set `COMPOSE_PROJECT_NAME` when running
another independent installation. Keep it unchanged when switching between
source and published images so the same named volumes are reused.

| Container path | Contents | Default storage |
| --- | --- | --- |
| `/usr/project/data` | Database, session recovery state and track cache | Project's `data` named volume |

The image includes FFmpeg, Node and a pinned yt-dlp installed in a Python virtual
environment. Cache and read-ahead settings use application defaults. Change `YTDLP_VERSION` as a Docker
build argument when deliberately testing another downloader version.

For host-visible storage, set `DATA_DIR=./data` in `.env`. Relative paths are
resolved from `compose.yaml`; absolute paths are also supported. The default
root user can use existing local data without changing its ownership. The
mount must be writable. Run only one bot process against each datastore
directory.

Named volumes need no host-directory ownership setup on a fresh installation.
Back up the whole data volume with the bot stopped, along with `.env`.
Do not use `docker compose down -v` when retaining application state. An image
rollback does not automatically reverse a database or configuration migration.

## Update, verify and roll back

For a published deployment, select the intended `IMAGE_TAG` in `.env`, then:

```sh
docker compose pull
docker compose up -d
docker compose logs --tail=100 app
```

Pulling happens before replacement. For source deployment, select the intended
Git revision and repeat the explicit build and `up --no-build` commands above.
Replacing the container briefly stops the bot. Both flows retain old images
and leave unrelated Docker images alone.

Check Discord command registration and persisted settings, then join a voice channel and test playback, `/queue` and `/stop`. A running
container alone does not establish Discord readiness. The image smoke checks
exercise offline packaging and persistence, without using a bot token.

To return to a published image after a local build, use only `compose.yaml`,
select the previous release tag, pull it and recreate the service. Keep a known
working Git revision for local builds; the `-local:dev` tag is replaced by the
next build. Check data compatibility before downgrading and restore a compatible
data backup when required.

## Image publishing and checks

The `Container` workflow tests Go code and renders the Compose configurations,
then builds native amd64 and arm64 images. Each image is checked offline for
its packaged assets, root execution, persistent storage and media tools.
CI publishes the public multi-platform tags after both architectures pass.

The Docker Hub account is `keshon`. Add a Docker Hub personal access token with
Read, Write & Delete permission as the GitHub repository Actions secret
`DOCKERHUB_TOKEN` to support both image publishing and overview sync. Image
publishing alone needs only Read & Write. Do not put the token in
`.env`, source files or workflow YAML. Fork pull requests build and check images
without publishing or using that secret.

Pushes to `main` publish `edge`; `v*` tags publish the exact tag, and tags without
a prerelease suffix also update `latest`. Every published build has a full
`sha-<commit>` tag. Architecture-specific tags are used while assembling the
manifest. Workflow dispatch builds and publishes the selected revision without
changing `latest` unless it is a stable release tag. Rebuilds can update OS
packages, so use a registry digest when an exact image must be retained.

Local verification from the repository root:

```sh
bash docker/check-compose.sh
docker build -f docker/Dockerfile -t melodix:check .
bash docker/check-image.sh melodix:check
```

The checks do not connect to Discord or certify playback, command permissions
or compatibility with an existing datastore. Verify those after deployment.
