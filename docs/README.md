# Melodix documentation

Start with a released binary, play a track, then change settings as needed.
These pages describe the development source; each release bundles its own docs.

## Run and use Melodix

- [Running Melodix](running.md): install on Windows, Linux or macOS; configure a
  Discord bot or start the terminal player; verify playback.
- [Playback](playback.md): search, play playlists, manage the queue and replay history.
- [Troubleshooting](troubleshooting.md): missing commands, silent playback,
  interrupted streams, storage locks and container settings.
- [Docker](../docker/README.md): build and operate a container with persistent data.
- [Configuration](running.md#configuration) and
  [backup/update procedures](running.md#persistent-data-and-restart-behavior).

## Change or embed Melodix

- [Conventions](conventions.md): local checks, extension steps and release workflow.
- [Architecture](architecture.md): components, playback, recovery and persistence.
- [Ownership](ownership.md): lifecycle and concurrency contracts and their checks.
- [Go music library](../pkg/music/README.md): use the playback engine in another program.
- [Brand assets](brand.md): mark, favicon sources and small-size constraints.

The [repository README](../README.md) introduces the project and includes the
generated command catalog. Runtime `help` and Discord `/help flat` list commands
for the version you are running.
