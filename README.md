![Melodix banner](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![Go Reference](https://pkg.go.dev/badge/github.com/keshon/melodix.svg)](https://pkg.go.dev/github.com/keshon/melodix) [![Release](https://img.shields.io/github/v/release/keshon/melodix)](https://github.com/keshon/melodix/releases) [![License](https://img.shields.io/github/license/keshon/melodix)](LICENSE)

# Melodix

A self-hosted Discord music bot written in Go, with a terminal player thrown in.
It streams YouTube, SoundCloud and internet radio, and it's built around one
stubborn idea: **playback should survive** — flaky streams, dead voice
connections, gateway reconnects, all of it.

Public music bots come and go, usually with a cease-and-desist attached.
Melodix is the opposite deal: a small binary you run yourself, with your own
token, on your own machine. Nobody can turn it off for you.

## What it does well

- **Refuses to drop a track.** Every track has several extraction backends
  (yt-dlp, kkdai, plain ffmpeg). If a stream dies mid-play, Melodix reopens it
  at the same position; if a backend keeps failing, it falls through to the next.
- **Survives Discord too.** A silent gateway or a dead voice connection is
  detected and recovered automatically, and queues live through session restarts.
- **Keeps a memory.** `/history` shows what was played; `/play 42` replays
  entry 42. No link hunting.
- **Stays small.** One binary plus ffmpeg (yt-dlp recommended). Storage is a
  single JSON file — no database to babysit.
- **Doubles as a terminal player.** The same engine drives `melodix-cli`,
  which plays straight to your speakers. Good for testing, also just for listening.

## Try it

The bot lives in the [Ctrl+Z](https://discord.gg/uDnTenPxAY) Discord server —
hop into a voice channel and use slash commands in `#music-spam`.

Prebuilt binaries are on the [releases page](https://github.com/keshon/melodix/releases).

## Quick start

```bash
# Discord bot — token from the Discord Developer Portal
go build -o melodix-discord ./cmd/discord
DISCORD_TOKEN=your-token ./melodix-discord

# ...or the terminal player, no Discord account required
go build -o melodix-cli ./cmd/cli
./melodix-cli
```

You need FFmpeg in `PATH`; yt-dlp is optional but recommended. The full setup
guide — creating the bot, invite link, every config knob, Docker — is in
[docs/running.md](docs/running.md).

## Commands

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

- **/maintenance** — Bot maintenance commands
  - **/maintenance ping** — Check bot latency
  - **/maintenance download-db** — Download the current server database as a JSON file
  - **/maintenance status** — Retrieve statistics about the guild
- **/settings** — Server settings
  - **/settings commands log** — Review recently used commands
  - **/settings commands status** — Show enabled and disabled command groups
  - **/settings commands enable** — Enable a command group
  - **/settings commands disable** — Disable a command group


<!-- /generated -->

`/play` takes more than links:

```text
/play never gonna give you up                       search query (YouTube)
/play https://www.youtube.com/watch?v=dQw4w9WgXcQ   direct link (YouTube / SoundCloud)
/play http://stream-uk1.radioparadise.com/aac-320   internet radio stream
/play 42                                            replay entry 42 from /history
```

## Under the hood

The playback engine ([pkg/music](pkg/music)) is a standalone Go library with
no Discord in it: resolver → queue → recovery stream → sink. The Discord bot
is one consumer of it, the CLI is another. If you're curious how the parser
fallback and voice recovery actually work, [docs/architecture.md](docs/architecture.md)
walks through the whole thing.

The codebase keeps itself honest: the house rules — naming, concurrency
contracts, how to add a source or parser — are written down in
[docs/conventions.md](docs/conventions.md), and CI enforces the mechanical
half (vet, race-enabled tests, a lint config that passes with zero findings).

## License

[MIT](LICENSE)
