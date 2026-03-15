![# Header](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![GoDoc](https://godoc.org/github.com/keshon/melodix?status.svg)](https://godoc.org/github.com/keshon/melodix) [![Go report](https://goreportcard.com/badge/keshon/melodix)](https://goreportcard.com/report/github.com/keshon/melodix)

# Melodix — Self-hosted Discord music bot & CLI player

Melodix is a music player you can run as a **Discord bot** (plays in voice channels) or as a **CLI app** (plays on your machine). Same playback engine for both: YouTube, SoundCloud, and internet radio. Written in Go.

---

## What Melodix does

- **Discord bot** — Add it to your server; join a voice channel and use slash commands to play from a link or search.
- **DAVE voice encryption** — Uses Discord’s current voice protocol (DAVE), so the bot works with today’s Discord clients and infrastructure. Actively maintained to stay compatible.
- **CLI player** — Run a terminal app and play the same sources locally (play, next, stop, queue) without Discord.
- **Multiple sources** — YouTube (by link or search), SoundCloud, and direct radio/stream URLs.
- **Queue and controls** — Play, skip to the next track, or stop and clear the queue (same in Discord and CLI).
- **Multiple servers** — One bot instance can serve many Discord servers.

**Limitations:** Cannot play YouTube live streams or region-locked videos. Not every radio stream format is supported. Playback may occasionally pause or vary slightly when retrying with a different stream method.

---

## Try Melodix without installing

- **Official Discord server** — Join the [Melodix Discord server](https://discord.gg/NVtdTka8ZT), go to a voice channel and use the slash commands at the `#bot-music-spam` text channel.
- **Pre-built binaries** — Download a [release](https://github.com/keshon/melodix/releases). Each archive includes **melodix-discord** (bot) and **melodix-cli** (terminal player). For the bot, create an app in the Developer Portal and run the Discord binary; for local playback, run the CLI binary (no Discord token needed).

---

## Commands

### 🕯️ Information

- **/about** — Discover the origin of this bot
- **/help** — Get a list of available commands
  - **/help category** — View commands grouped by category
  - **/help group** — View commands grouped by group
  - **/help flat** — View all commands as a flat list

### 🎵 Music

- **/music** — Control music playback
  - **/music play** — Play a music track
  - **/music next** — Skip to the next track
  - **/music stop** — Stop playback and clear queue

### ⚙️ Settings

- **/commands** — Manage or inspect commands
  - **/commands log** — Review recent commands called by users
  - **/commands status** — Check which command groups are enabled or disabled
  - **/commands toggle** — Enable or disable a group of commands
  - **/commands update** — Re-register or update slash commands
- **/maintenance** — Bot maintenance commands
  - **/maintenance ping** — Check bot latency
  - **/maintenance download-db** — Download the current server database as a JSON file
  - **/maintenance status** — Retrieve statistics about the guild


**Music examples:** Play by search query or by link. You must be in a voice channel to use `/music play`.

```
/music play Never Gonna Give You Up
/music play https://www.youtube.com/watch?v=dQw4w9WgXcQ
/music play http://stream-uk1.radioparadise.com/aac-320
```

---

## Running Melodix yourself

You can run the **Discord bot** (voice channels) or the **CLI player** (local playback). Both use the same sources and queue logic.

### What you need (both modes)

- **FFmpeg** on your system PATH (used for some audio streams).
- **yt-dlp** (optional but recommended) on your PATH for better YouTube support.

For the **Discord bot** you also need a **Discord bot token** from the [Discord Developer Portal](https://discord.com/developers/applications).

### Discord bot — Step 1: Create the bot in Discord

1. Open the [Discord Developer Portal](https://discord.com/developers/applications) and create a new application. Note the **Application ID** (in the “General Information” section).
2. Go to the **Bot** section and create a bot. Copy the **token** (you will use it as `DISCORD_TOKEN`).
3. Under **Privileged Gateway Intents**, enable:
   - Presence Intent  
   - Server Members Intent  
   - Message Content Intent  
4. Invite the bot to your server using this URL (replace `YOUR_APPLICATION_ID` with your Application ID from step 1):

   `https://discord.com/oauth2/authorize?client_id=YOUR_APPLICATION_ID&scope=bot&permissions=3238912`

   This link only requests the permissions the bot needs: View Channel, Send Messages, Embed Links, Read Message History, Manage Messages, Connect to Voice Channel, Speak.

5. Open the URL in your browser, choose your server, and authorize. Grant the requested permissions when asked.

### Discord bot — Step 2: Configure and run

Create a `.env` file in the folder where you run the bot (or set the same variables in your environment):

```env
# Required for the Discord bot
DISCORD_TOKEN=your-discord-bot-token
```

Optional variables (you can add these to `.env` if needed):

| Variable | Description | Default |
|----------|-------------|---------|
| `STORAGE_PATH` | Path for bot data (e.g. command state). | `./data/datastore.json` |
| `INIT_SLASH_COMMANDS` | Set to `true` to register slash commands on every startup. | `false` |
| `DEVELOPER_ID` | Your Discord user ID for developer-only commands. | (none) |
| `DISCORD_GUILD_BLACKLIST` | Comma-separated guild IDs the bot will leave. | (none) |

**Run the Discord bot:**
- **From source:** `go build -o melodix-discord ./cmd/discord` then run the binary. Ensure `DISCORD_TOKEN` is set (e.g. in `.env`).
- **From a release:** Use the `melodix-discord` (or `melodix-discord.exe`) binary from the [releases](https://github.com/keshon/melodix/releases) archive.
- **With Docker:** See [docker/README.md](docker/README.md) for Docker and Docker Compose instructions.

After the bot is running and invited to your server, use slash commands in any channel. For music, be in a voice channel and use `/music play` with a link or search term.

### CLI player

The CLI player uses the same playback engine but runs in your terminal and plays through your speakers. No Discord token or server setup required.

**Run the CLI player:**
- **From source:** `go build -o melodix-cli ./cmd/cli` then run the binary.
- **From a release:** Use the `melodix-cli` (or `melodix-cli.exe`) binary from the [releases](https://github.com/keshon/melodix/releases) archive.

**CLI commands** (at the `> ` prompt):

| Command | Description |
|---------|-------------|
| `play <url or query> [source] [parser]` | Add and play (e.g. `play https://youtube.com/...` or `play Never Gonna Give You Up`). |
| `next` | Skip to the next track. |
| `stop` | Stop playback and clear the queue. |
| `queue` | Show now playing and the queue. |
| `status` | Show current track and queue length. |
| `quit` | Exit. |

Example:

```
> play Never Gonna Give You Up
> queue
> next
> quit
```

---

## Support

For help or questions, use the [Melodix Discord server](https://discord.gg/NVtdTka8ZT).

---

## License

Melodix is licensed under the [MIT License](https://opensource.org/licenses/MIT).
