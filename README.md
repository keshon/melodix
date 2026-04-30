
![# Header](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![Go Reference](https://pkg.go.dev/badge/github.com/keshon/melodix.svg)](https://pkg.go.dev/github.com/keshon/melodix) [![Go Report Card](https://goreportcard.com/badge/github.com/keshon/melodix)](https://goreportcard.com/report/github.com/keshon/melodix) [![Release](https://img.shields.io/github/v/release/keshon/melodix)](https://github.com/keshon/melodix/releases) [![License](https://img.shields.io/github/license/keshon/melodix)](LICENSE)


# Melodix

Self-hosted Discord music bot with a CLI player, built in Go.

Designed to run for long sessions with minimal failure rate.

---

## Quick start

### Run Discord bot

1. Create a bot in Discord Developer Portal
2. Get your token
3. Run:

```bash
go build -o melodix-discord ./cmd/discord
DISCORD_TOKEN=your-token ./melodix-discord
````

Full setup guide: see `docs/running.md`

---

### Run CLI player

```bash
go build -o melodix-cli ./cmd/cli
./melodix-cli
```

---

## Features

* Discord bot and CLI player share the same playback engine
* Multiple parsers with automatic fallback (yt-dlp, kkdai, ffmpeg)
* Recovery streams for unstable or broken sources
* Queue system per guild (Discord) or per process (CLI)
* Fully self-hosted

---

## Try Melodix

### Use the official server

Try the bot in [Ctrl+Z](https://discord.gg/uDnTenPxAY) Discord server: 
enter voice channel and use slash commands in `#bot-music-spam`.

---

### Download a release

Download pre-built binaries:

https://github.com/keshon/melodix/releases

---

## Commands (Discord)

<!-- generated -->

### 🕯️ Information

- **/about** — Discover the origin of this bot
- **/help** — Get a list of available commands
  - **/help category** — View commands grouped by category
  - **/help group** — View commands grouped by group
  - **/help flat** — View all commands as a flat list

### 🎵 Music

- **/history** — Show recently played tracks (replay by id with /play)
- **/next** — Skip to the next track
- **/play** — Play a music track
- **/stop** — Stop playback and clear queue

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


<!-- /generated -->

Example usage:

```bash
/play Never Gonna Give You Up
/play https://www.youtube.com/watch?v=dQw4w9WgXcQ
/play http://stream-uk1.radioparadise.com/aac-320
/play 42
/history
```

---

## Running

Requirements:

* FFmpeg in PATH
* yt-dlp (optional, recommended)

For full setup (Discord bot, env config, Docker):
see `docs/running.md`

---

## Documentation

* Running and setup: `docs/running.md`
* Architecture: `docs/architecture.md`

---

## License

Melodix is licensed under the [MIT License](https://opensource.org/licenses/MIT).
