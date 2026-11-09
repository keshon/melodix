# Running Melodix

Run Melodix from an extracted release or build it from source. The Discord bot
needs your bot token; the terminal player plays through your speakers without a
Discord account. Start one track before changing optional settings.

- [Set up the bot](#discord-bot)
- [Play through your speakers](#cli-player)
- [Build from source](#build-from-source)
- [Run in Docker](#docker)
- [Back up or update](#persistent-data-and-restart-behavior)
- [Look up a setting](#configuration)
- [Diagnose a failure](troubleshooting.md)

## Requirements

Download the archive for your operating system and CPU from the
[releases page](https://github.com/keshon/melodix/releases). Extract the whole
archive, including `.env` and `assets/`. Run commands from the extracted folder:
relative configuration, data and image paths are resolved from that directory.

The native YouTube Opus path needs no external media programs. Install FFmpeg on
`PATH` for SoundCloud, radio and transcoding fallbacks. YouTube live broadcasts
also need yt-dlp and a JavaScript runtime; see [optional dependencies](#yt-dlp-and-javascript).
Building from source requires Go 1.26+ and the platform's audio build dependencies.

These online guides describe the development source. For an installed release,
use the documentation bundled with that release; features can differ.

## Discord bot

### Step 1: Create the bot

1. Open the [Discord Developer Portal](https://discord.com/developers/applications).
2. Create an application and open its **Bot** settings.
3. Copy the bot token. Keep it private; never paste it into chat or commit it.
4. Enable **Server Members Intent**. Permission checks use the member cache.
   Presence and Message Content intents are not required.

### Step 2: Invite it to a server

Replace `YOUR_APPLICATION_ID` with your application ID and open:

```text
https://discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=3165184
```

The invitation requests View Channel, Send Messages, Embed Links, Connect and
Speak. Channel overrides can still refuse these permissions; check the voice
and command channels you intend to use.

### Step 3: Configure

Edit the supplied `.env` file in the extracted folder. Replace its example token:

```env
DISCORD_TOKEN=your-token
INIT_SLASH_COMMANDS=true
```

Enable registration for the first start and when updating command definitions.
It registers commands for the bot's servers at startup. The code default is
`false`; the supplied example enables it for first use. Leave other settings at
their supplied values until playback works.

### Step 4: Run it

In PowerShell on Windows, open the extracted folder and run:

```powershell
.\melodix-discord.exe
```

On Linux or macOS, from the extracted folder:

```sh
chmod +x melodix-discord
./melodix-discord
```

Leave the terminal open while the bot runs. For unattended operation, use your
system's service supervisor or the [Docker deployment](../docker/README.md).
The program recovers Discord sessions; it does not restart its own process.

## Verify the first start

1. Join a voice channel on the server where you invited the bot.
2. Run `/play` with a YouTube video link and confirm you hear audio.
3. Run `/queue`, then `/stop`. Playback should stop and the queue should clear.
4. Use `/help flat` for commands, or continue with [Playback](playback.md).

If commands are missing or the bot is silent, use
[Troubleshooting](troubleshooting.md). A successful connection check alone does
not verify voice playback.

## CLI player

The terminal player does not require a Discord token. From the extracted folder:

```powershell
# Windows PowerShell
.\melodix-cli.exe
```

```sh
# Linux or macOS
chmod +x melodix-cli
./melodix-cli
```

At its prompt, enter `help`, `play <link>`, `queue`, then `stop`. Confirm audio
plays through your speakers; `quit` exits. The [playback guide](playback.md#terminal-player)
explains query and source selection. If the bot already owns the same storage
directory, the CLI runs without persistent history and with an in-memory cache index.

## Build from source

Run these commands from a source checkout, not an extracted release. Install
Go 1.26+. On Linux, the speaker dependency needs ALSA development files and a
C compiler; the project's Ubuntu build installs `build-essential libasound2-dev`.

```sh
go build -o melodix-discord ./cmd/discord
go build -o melodix-cli ./cmd/cli
```

On Windows, use output names `melodix-discord.exe` and `melodix-cli.exe` instead.
Copy `.env.example` to `.env`, replace the token for bot use, and follow the
launch and verification steps above. The CLI can run without a token.

## Docker

The [Docker guide](../docker/README.md) owns container build commands, environment
forwarding, updates and mounted paths. It includes the optional media programs.
Its supplied read-ahead setting is 10 seconds rather than the application's
30-second default.

## yt-dlp and JavaScript

For YouTube live broadcasts and the yt-dlp fallback, install yt-dlp and one of
Node, Deno or Bun on `PATH`. The Docker image includes Node, FFmpeg and yt-dlp.
No additional Melodix setting is required for runtime detection.

The first yt-dlp invocation logs which path was selected:

```text
ytdlp_youtube_client_configured js_runtime=node player_client=web_embedded
ytdlp_no_js_runtime_live_streams_will_fail looked_for=["deno","node","bun"]
```

A missing runtime can leave ordinary videos working while live streams stop
after their first segments. Check the warning before changing recovery settings.

## Proxies

The native HTTP clients use `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY`. For example,
set this in the process environment or `.env` before starting Melodix:

```env
HTTPS_PROXY=http://proxy.example:8080
```

FFmpeg and yt-dlp inherit the process environment. FFmpeg reads lowercase
`http_proxy`, so also set that variable when proxying its requests. To bypass an
HTTP proxy for Discord hosts:

```env
NO_PROXY=discord.com,discordapp.com,discord.media
```

This bypasses the proxy, not a VPN. Voice packets use UDP directly to Discord;
an HTTP proxy does not carry them. Confirm each tool's proxy behavior on the
network where the bot will run.

## Persistent data and restart behavior

`STORAGE_PATH` holds history, server settings and the cache index in a datastore
directory with a lock, write-ahead log and snapshots. One process owns it at a time.

To back up: stop Melodix with Ctrl+C, wait for it to exit, then copy the whole
storage directory. To restore: stop it, retain the current directory as a rollback
copy, replace it with the backup and start the matching release. Verify playback
and settings before returning the instance to normal use.

For an update, extract the new release into a separate folder. Stop the old
process, back up its storage, and carry your edited `.env` and data into the new
installation (or retain their configured absolute paths). Keep the new `assets/`
with its binary. Enable command registration if definitions changed. Verify the
new instance before removing the old installation. To roll back, stop the new
process and use the old release with its pre-update data backup; do not assume
data written by a newer version is backward compatible.

The optional audio cache lives at `CACHE_DIR`, separate from history.
`CACHE_PERSISTENT=false` clears it on startup. Stop playback before manually
clearing a cache directory. Docker paths must stay under its mounted data folder.

Discord session reconnects preserve the in-memory queue. A full process restart
clears it. Neither history nor the track cache restores a pending queue.

## Configuration

Values below are the code defaults, not necessarily the values in an edited
`.env` or container environment. Restart the process after changing settings.
Environment variables already set for the process take precedence over `.env`.
The examples deliberately enable initial command registration; Docker also
overrides storage paths and read-ahead.

### Bot and storage

| Variable | Description | Default |
|---|---|---|
| `DISCORD_TOKEN`           | The bot token from Step 1. Required; the bot exits without it. | (none) |
| `STORAGE_PATH`            | Directory the datastore owns (write-ahead log + snapshots). Locked to one process. | `./data/store` |
| `INIT_SLASH_COMMANDS`     | Set to `true` to register slash commands on every startup. | `false`                 |
| `DISCORD_GUILD_BLACKLIST` | Comma-separated guild IDs the bot will leave on sight.      | (none)                  |
| `DEVELOPER_ID` | Your Discord user ID. Runs every command on every server regardless of roles — for running /maintenance (with its database dump) and /settings without an admin role. Treat as a credential; empty disables it. | (none) |

### Playback and cache

| Variable | Description | Default |
|---|---|---|
| `VOICE_READY_DELAY_MS`    | Delay after joining voice before checking whether the channel uses end-to-end encryption, in milliseconds. | `500` |
| `CACHE_ENABLED`           | Cache completed tracks to disk for later playback without extraction. Radio streams are not cached. | `false` |
| `CACHE_DIR`               | Where cache blobs live (wiped on boot unless persistent).   | `./data/cache`           |
| `CACHE_MAX_BYTES`         | Global cache size cap; oldest-used tracks get evicted once it's hit. | `2147483648` (2 GiB) |
| `CACHE_PERSISTENT`        | Keep the cache across restarts, or wipe it on every boot (`false`). | `true`             |
| `BUFFER_AHEAD_MS`         | Read-ahead depth in milliseconds; `0` disables it. The buffer plays through short stalls without delaying startup. | `30000` |
| `MAX_AUDIO_BITRATE`       | Maximum format bitrate selected by the native YouTube parser, in bits per second. `0` selects the best available format. | `0` |

### Recovery and command scheduling

| Variable | Description | Default |
|---|---|---|
| `WS_SILENCE_TIMEOUT`      | How long without events or heartbeat ACKs before the gateway is treated as unhealthy. | `2m`  |
| `DISCORD_UNHEALTHY_MODE`  | What to do when unhealthy: `restart-session`, `restart-voice`, or `ignore`. | `restart-session` |
| `DISCORD_UNHEALTHY_GRACE` | Under `restart-session`, ignore the first N unhealthy signals in the window below (sinks still get invalidated). | `0` |
| `DISCORD_UNHEALTHY_WINDOW`| Window used to count unhealthy signals. It must span the grace count of silence timeouts for escalation to occur. | `1m` |
| `PLAYER_TRANSPORT_RECOVERY_MODE` | On a voice transport failure: `hard` rejoins the voice channel outright, `soft` tries reopening the stream first and falls back to hard. | `hard` |
| `PLAYER_TRANSPORT_SOFT_ATTEMPTS` | In `soft` mode, how many soft retries happen before falling back to hard. | `1` |
| `COMMAND_PARALLELISM`     | Maximum handlers across all servers. Commands within one server remain sequential. | `16` |

### Logging

| Variable | Description | Default |
|---|---|---|
| `LOG_LEVEL`               | `trace`, `debug`, `info`, `warn`, `error`, `fatal` or `panic`. The Discord library is logged at the same level, so `debug` is loud. | `info` |
| `LOG_FILE`                | Path to a rotated JSON log. Empty means stderr only, pretty-printed. | (none) |
| `LOG_MAX_SIZE_MB`         | Rotate the log file once it reaches this size.              | `10` |
| `LOG_MAX_BACKUPS`         | How many rotated files to keep.                             | `3` |
| `LOG_MAX_AGE_DAYS`        | Delete rotated files older than this; `0` keeps them by count alone. | `0` |
| `LOG_COMPRESS`            | Gzip rotated files.                                         | `false` |

### Tune a constrained connection

Read-ahead builds only when the source delivers faster than playback. Raising
`BUFFER_AHEAD_MS` lets an accumulated lead cover longer stalls; it does not
pre-fill before playback. It uses roughly 17 KB per buffered second at a typical
YouTube bitrate, per playing server.

`MAX_AUDIO_BITRATE` caps the native YouTube format selection. With encrypted
voice, Discord relays the selected audio without transcoding, so that bitrate
also affects listeners. A cap trades source quality for bandwidth and reduces
bytes re-fetched after a dropped source. It is not a volume setting.

If `DISCORD_UNHEALTHY_GRACE` is nonzero, its counting window must span that many
silence timeouts plus some margin. A window too short prevents escalation; the
bot reports `unhealthy_grace_never_escalates` when the settings cannot work.
