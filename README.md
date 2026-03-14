![# Header](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![GoDoc](https://godoc.org/github.com/keshon/melodix?status.svg)](https://godoc.org/github.com/keshon/melodix) [![Go report](https://goreportcard.com/badge/keshon/melodix)](https://goreportcard.com/report/github.com/keshon/melodix)

# Melodix — Self-hosted Discord music bot

Melodix is a Discord bot that plays music in voice channels. You can add it to your own server and run it yourself, or try it on the official server. It is written in Go and supports YouTube, SoundCloud, and internet radio streams.

---

## What Melodix does

- **Plays audio in voice channels** — Join a voice channel, use the music commands, and the bot streams audio from the given link or search.
- **DAVE voice support** — Uses Discord’s voice encryption (DAVE) so audio works with current Discord clients and infrastructure.
- **Multiple sources** — YouTube (by link or search), SoundCloud, and direct radio/stream URLs.
- **Queue and controls** — Play, skip to the next track, or stop and clear the queue.
- **Multiple servers** — One bot instance can serve many Discord servers.
- **Slash commands** — All features are available via Discord slash commands (e.g. `/music play`, `/help`).

**Limitations:** The bot cannot play YouTube live streams or region-locked videos. Not every radio stream format is supported. Playback may occasionally pause or vary slightly when the bot retries with a different stream method.

---

## Try Melodix without installing

- **Official Discord server** — Join the [Melodix Discord server](https://discord.gg/NVtdTka8ZT), go to a voice channel and use the slash commands at the `#bot-music-spam` text channel.
- **Pre-built binaries** — Download a [release](https://github.com/keshon/melodix/releases), then follow the setup steps below to create a bot in the Developer Portal and run the binary.

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

This section is for anyone who wants to host the bot on their own machine or server.

### What you need

- A **Discord bot token** (from the Discord Developer Portal).
- **FFmpeg** installed and on your system PATH (used for some audio streams).
- **yt-dlp** (optional but recommended) on your PATH for better YouTube support. Without it, the bot falls back to other methods.

### Step 1 — Create the bot in Discord

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

### Step 2 — Configure and run

Create a `.env` file in the folder where you run the bot (or set the same variables in your environment):

```env
# Required: your bot token from the Developer Portal
DISCORD_TOKEN=your-discord-bot-token
```

Optional variables (you can add these to `.env` if needed):

| Variable | Description | Default |
|----------|-------------|---------|
| `STORAGE_PATH` | Path for bot data (e.g. command state). | `./data/datastore.json` |
| `INIT_SLASH_COMMANDS` | Set to `true` to register slash commands on every startup. | `false` |
| `DEVELOPER_ID` | Your Discord user ID for developer-only commands. | (none) |
| `DISCORD_GUILD_BLACKLIST` | Comma-separated guild IDs the bot will leave. | (none) |

**Run the bot:**
Ensure `DISCORD_TOKEN` is set (e.g. in `.env`).
- **From source (Go):** Clone the repo, run `go build ./cmd/discord`.
- **From a release:** Download the pre-built binary from [releases](https://github.com/keshon/melodix/releases).
- **With Docker:** See [docker/README.md](docker/README.md) for Docker and Docker Compose instructions.

After the bot is running and invited to your server, use slash commands in any channel where the bot can read and send messages. For music, be in a voice channel and use `/music play` with a link or search term.

---

## Support

For help or questions, use the [Melodix Discord server](https://discord.gg/NVtdTka8ZT).

---

## License

Melodix is licensed under the [MIT License](https://opensource.org/licenses/MIT).
