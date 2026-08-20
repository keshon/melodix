![Melodix banner](https://raw.githubusercontent.com/keshon/melodix/master/assets/readme-banner.webp)

[![Go Reference](https://pkg.go.dev/badge/github.com/keshon/melodix.svg)](https://pkg.go.dev/github.com/keshon/melodix) [![Release](https://img.shields.io/github/v/release/keshon/melodix)](https://github.com/keshon/melodix/releases) [![License](https://img.shields.io/github/license/keshon/melodix)](LICENSE)

# Melodix

A self-hosted Discord music bot written in Go, with a terminal player thrown in.
It streams YouTube, SoundCloud and internet radio, and it's built around one
stubborn idea: playback should survive — flaky streams, dead voice
connections, gateway reconnects, all of it.

Public music bots tend to disappear eventually, usually with a
cease-and-desist attached. Melodix skips that risk: it's a small binary you
run yourself, with your own token, on your own machine. Nobody can turn it
off for you.

## What it does well

- Melodix refuses to drop a track. Every track has several extraction
  backends behind it (native extractors, kkdai, yt-dlp). If a stream dies
  mid-play, it reopens at the same position; if a backend keeps failing, it
  falls through to the next one.
- It survives Discord too — a silent gateway or a dead voice connection
  gets detected and recovered automatically, and queues live through
  session restarts.
- It keeps a memory: `/history` shows what was played, and `/play 42`
  replays entry 42. No digging through chat for the original link.
- It stays small. Just one binary, and for YouTube alone that's genuinely
  all you need — no ffmpeg required. Add ffmpeg for SoundCloud and internet
  radio, and yt-dlp as a last-resort fallback if you want the extra
  reliability. Storage is a single JSON file, no database to babysit.
- It doubles as a terminal player. The same engine drives `melodix-cli`,
  which plays straight to your speakers — handy for testing, or just for
  listening.

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

FFmpeg is only needed for SoundCloud and internet radio — a YouTube-only bot
doesn't need it at all. yt-dlp is optional too, used as a last-resort
fallback. The full setup guide — creating the bot, invite link, every config
knob, Docker — is in [docs/running.md](docs/running.md).

## Commands

<!-- generated -->

### ℹ️ Information

- **/about** — Discover the origin of this bot
- **/help** — Get a list of available commands
  - **/help category** — View commands grouped by category
  - **/help group** — View commands grouped by group
  - **/help flat** — View all commands as a flat list

### 🎵 Music

- **/history** — Show recently played tracks (replay by id with /play)
- **/next** — Skip to the next track
- **/play** — Play a music track
- **/queue** — Show what is playing and what is queued next
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
/play https://www.youtube.com/playlist?list=PL...   whole YouTube playlist
/play https://www.youtube.com/watch?v=...&list=RD   YouTube mix / radio
/play http://stream-uk1.radioparadise.com/aac-320   internet radio stream
/play 42                                            replay entry 42 from /history
```

A playlist link queues the whole list, up to 100 tracks; `/queue` shows what
is waiting. A link that names both a video and a playlist
(`watch?v=...&list=PL...`) plays just that video — the one exception is a mix,
which has no other link shape.

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

[MIT](LICENSE) © Innokentiy Sokolov (Señor Mega / Big M)
