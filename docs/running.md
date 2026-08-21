# Running Melodix

Melodix runs two ways: as a Discord bot, or as a standalone CLI player. This
guide covers both.

---

## Requirements

- FFmpeg in `PATH` (optional if YouTube only)
- yt-dlp (recommended, not required)

### yt-dlp and JavaScript

Install a JavaScript runtime — Node, Deno or Bun — and leave it on `PATH`.
That is the whole requirement; nothing needs configuring.

Without one, yt-dlp cannot solve YouTube's challenges and falls back to a
client googlevideo serves under restrictions. Ordinary videos still play, so
the gap is easy to miss, but live broadcasts stop after twenty to forty
seconds as their segments start answering 403. With a runtime present the bot
selects the embedded web client itself on every yt-dlp call, which is what
avoids that.

The log says which way it went, on the first yt-dlp call:

```
ytdlp_youtube_client_configured js_runtime=node player_client=web_embedded
ytdlp_no_js_runtime_live_streams_will_fail looked_for=["deno","node","bun"]
```

The Docker image ships Node, so it needs nothing further.

---

## Discord bot

### Step 1: Create the bot

1. Open https://discord.com/developers/applications
2. Create a new application
3. Go to the "Bot" tab
4. Create the bot and copy its token — you'll need it in a moment

While you're there, enable these intents:
- Presence
- Server Members
- Message Content

### Step 2: Invite it to a server

Swap in your own application ID and open this URL:

https://discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=3238912

### Step 3: Configure

Create a `.env` file (or just export the variables directly):

```env
DISCORD_TOKEN=your-token
```

That's the only one that's required. Everything else below has a sane
default and can be left alone until you actually need it:

| Variable                  | Description                                                | Default                 |
| ------------------------- | ---------------------------------------------------------- | ------------------------ |
| `STORAGE_PATH`            | Directory the datastore owns (write-ahead log + snapshots). Locked to one process. | `./data/store` |
| `INIT_SLASH_COMMANDS`     | Set to `true` to register slash commands on every startup. | `false`                 |
| `DEVELOPER_ID`            | Your Discord user ID, for developer-only commands.          | (none)                  |
| `DISCORD_GUILD_BLACKLIST` | Comma-separated guild IDs the bot will leave on sight.      | (none)                  |
| `VOICE_READY_DELAY_MS`    | Delay after joining a voice channel before sending Opus (avoids an OP4 race). | `500`         |
| `WS_SILENCE_TIMEOUT`      | How long without events or heartbeat ACKs before the gateway is treated as unhealthy. | `2m`  |
| `DISCORD_UNHEALTHY_MODE`  | What to do when unhealthy: `restart-session`, `restart-voice`, or `ignore`. | `restart-session` |
| `DISCORD_UNHEALTHY_GRACE` | Under `restart-session`, ignore the first N unhealthy signals in the window below (sinks still get invalidated). | `0` |
| `DISCORD_UNHEALTHY_WINDOW`| The window `DISCORD_UNHEALTHY_GRACE` counts within.         | `1m`                    |
| `PLAYER_TRANSPORT_RECOVERY_MODE` | On a voice transport failure: `hard` rejoins the voice channel outright, `soft` tries reopening the stream first and falls back to hard. | `hard` |
| `PLAYER_TRANSPORT_SOFT_ATTEMPTS` | In `soft` mode, how many soft retries happen before falling back to hard. | `1` |
| `CACHE_ENABLED`           | Cache played tracks to disk, so later plays — from any guild, or via `/play <id>` — serve instantly with no re-extraction. | `false` |
| `CACHE_DIR`               | Where cache blobs live (wiped on boot unless persistent).   | `./data/cache`           |
| `CACHE_MAX_BYTES`         | Global cache size cap; oldest-used tracks get evicted once it's hit. | `2147483648` (2 GiB) |
| `CACHE_PERSISTENT`        | Keep the cache across restarts, or wipe it on every boot (`false`). | `true`             |
| `BUFFER_AHEAD_MS`         | Read-ahead depth in ms. The queued lead plays through a source stall or a reconnect, so on a lossy link this decides whether a dropped connection is audible. Costs roughly 17 KB per buffered second per guild at YouTube's usual bitrate — about 500 KB at the default depth — and does not pre-fill, so raising it delays nothing. Set to `0` to disable. | `30000` |
| `MAX_AUDIO_BITRATE`       | Cap on the YouTube audio format the native parser picks, in bits per second. The same track is usually offered near 49k, 66k and 137k, and a Discord voice channel carries 64 kbps unless the guild is boosted — so the top format mostly buys bandwidth the channel will not use. Worth setting on a slow link. `0` takes the best on offer. | `0` |
| `COMMAND_TIMEOUT`         | Hard timeout for a single command execution.                | `30s`                   |
| `COMMAND_PARALLELISM`     | Max number of command handlers running at once.             | `16`                    |

### Step 4: Run it

```bash
go build -o melodix-discord ./cmd/discord
./melodix-discord
```

---

## CLI player

No Discord account needed here — it plays straight to your speakers.

```bash
go build -o melodix-cli ./cmd/cli
./melodix-cli
```

Once it's running:

* `play <url or query>`
* `next`
* `stop`
* `queue`
* `status`
* `quit`

---

## Docker

Covered separately in `docker/README.md`.
