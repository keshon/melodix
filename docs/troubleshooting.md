# Troubleshooting

Start with the symptom below. Keep the release/version, operating system,
affected source and relevant log events when reporting a problem. Remove bot
tokens and private URLs before sharing logs.

## The bot does not connect

Check `DISCORD_TOKEN` in the environment used to start the process and confirm
**Server Members Intent** is enabled for that Discord application. Run from the
directory containing the intended `.env`; a terminal's existing environment can
override it. A missing file message means system environment variables are being used.

The bot has a connection diagnostic:

```powershell
# Windows, from the extracted folder
.\melodix-discord.exe -check
```

```sh
# Linux or macOS, from the extracted folder
./melodix-discord -check
```

This connects using the configured token, waits for gateway READY and reports
the identity, server count and heartbeat latency. It reads registered commands
from a cached server when one exists, then disconnects. It registers no commands
and sends no messages. Success does not verify voice UDP, encryption, audio,
or permissions in every server. With no cached servers, the command-list REST
check is skipped. Do not treat `-check` as an offline test.

## Slash commands are missing

Check that the bot was invited to the intended server and start it with
`INIT_SLASH_COMMANDS=true`. The code default is false; the release example enables
registration. Confirm registration after updating a release that changes commands.
Use `/help flat` once the commands are available.

## A command is refused

Check the caller's roles, channel permissions and whether the command's group
is disabled for this server. A server administrator can inspect
`/settings commands status`. Buttons on old search choosers also respect command
group restrictions. Do not use `DEVELOPER_ID` to bypass a permission diagnosis:
it grants its configured user every command across all servers.

## The bot connects but no audio plays

Join the intended voice channel, then check that the bot has **View Channel**,
**Connect** and **Speak** there, including channel overrides. Try a known-working
YouTube video and watch the Now Playing status and logs. A connection diagnostic
cannot test the voice path.

If tracks resolve but audio cannot reach Discord, check the host's outbound UDP
connectivity. HTTP proxy settings do not proxy voice packets. If only SoundCloud,
radio or fallback parsers fail, run `ffmpeg -version` in the same environment
that starts the bot. A terminal player needs a working local audio device.

## A track stops or a source fails

Separate a source failure from voice transport recovery. Record the parser and
events around `stream_opening`, `stream_opened` and `voice_transport_lost`.
Opening a process is not proof that it delivered audio; the confirmed-open event
follows the first packet. Try another track from the same source, then another
source, before changing recovery settings.

For a live YouTube stream, confirm yt-dlp and Node, Deno or Bun are on the bot's
`PATH`. Check for `ytdlp_no_js_runtime_live_streams_will_fail`. A missing runtime
can affect live streams while ordinary videos still work. See
[dependency setup](running.md#yt-dlp-and-javascript).

Read-ahead can cover brief stalls only after a lead builds. It cannot fix a
source consistently slower than playback. See [connection tuning](running.md#tune-a-constrained-connection).
Extractor behavior can change upstream; record the affected release and source
when checking for an update. Do not assume every 403 has the same cause.

## History is missing or the storage directory is locked

Only one process can own `STORAGE_PATH`. The CLI falls back to playback without
persistent history when another process owns it. Confirm which process is
running and which directory its configuration selects. Stop the owner cleanly
before moving or restoring data; do not delete its lock while it runs.

The pending queue is in memory. Restarting the process clears it even when
history is present. A cache directory is audio storage, not a queue backup.
Follow the [backup and restore procedure](running.md#persistent-data-and-restart-behavior).

## A Docker setting has no effect

Compose passes application variables explicitly. A key in `docker/.env` reaches
the process only if `docker-compose.yml` forwards it. Recreate the container
after changing its environment; restarting an unchanged container is not enough.

Check startup logs for `track_cache_enabled` or `track_cache_disabled`; these
also report effective read-ahead and bitrate settings. Avoid sharing a complete
environment dump, which contains the bot token. The supplied Docker read-ahead
value is 10000 ms; the application's code default is 30000 ms. `COMMAND_TIMEOUT`
in the current Compose file is not read by the application and has no effect.

See the [Docker guide](../docker/README.md) for mounted paths and update effects.

## Report an unresolved problem

Include the version/commit, host OS, source type, minimal steps and relevant log
events in a [GitHub issue](https://github.com/keshon/melodix/issues). State whether
the bot connects, commands respond and another track/source works. Never include
your token, a real `.env`, exported server data or unredacted private URLs.
