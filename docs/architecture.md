# Architecture

Melodix is a Discord music bot built around a reusable, Discord-agnostic playback engine.
The repository ships two binaries on top of the same engine:

- **`cmd/discord`** — the Discord bot (slash commands, voice, persistence, health watchdogs).
- **`cmd/cli`** — a REPL that plays to the local speaker. It exists both as a debugging tool
  and as proof that `pkg/music` has no Discord dependency.

```mermaid
flowchart TB
  subgraph Consumers
    DiscordBot["cmd/discord + internal/*"]
    CLI["cmd/cli"]
  end
  subgraph Engine["pkg/music (Discord-agnostic)"]
    Resolver["resolve.Resolver<br/>(input → TrackInfo)"]
    Player["player.Player<br/>(queue + playback loop)"]
    Stream["stream.RecoveryStream<br/>(parser fallback + retry)"]
    Parsers["parsers: kkdai | ytdlp | ffmpeg<br/>(track → PCM via ffmpeg)"]
    SinkIface["sink.AudioSink"]
  end
  DiscordBot --> Player
  CLI --> Player
  Player --> Resolver
  Player --> Stream
  Stream --> Parsers
  Player --> SinkIface
  SinkIface -->|Opus encode + VC send| DiscordVC["Discord voice"]
  SinkIface -->|oto| Speaker["Local speaker"]
```

---

## Package map

| Path | Responsibility |
|---|---|
| `pkg/music/player` | `Player`: FIFO queue, playback goroutine, transport recovery, status channel |
| `pkg/music/resolve` | `Resolver`: input → `[]TrackInfo`; source detection and precedence |
| `pkg/music/sources` | `Source` interface + `youtube`, `soundcloud`, `radio` implementations |
| `pkg/music/parsers` | `Streamer` interface + `kkdai`, `ytdlp`, `ffmpeg` implementations |
| `pkg/music/stream` | Parser registry, `RecoveryStream`, PCM constants (48 kHz / stereo / 960-sample frames) |
| `pkg/music/sink` | `AudioSink`/`Provider` interfaces + speaker implementation |
| `internal/discord` | The `Bot`: session lifecycle, handlers, health watchdogs, voice service |
| `internal/discord/voice` | Per-guild players and sink providers; guild status messages; **survives session restarts** |
| `internal/discord/voice/sink` | `DiscordSink`: PCM → Opus → voice connection |
| `internal/discord/cmdadapter` | Bridges melodix command types to the `keshon/command` registry/middleware framework |
| `internal/discord/cmdsync` | Per-guild slash-command diff sync (create/edit/delete) |
| `internal/discord/discordreply` | Embed/response helpers shared by handlers and the voice service |
| `internal/discord/execguard` | Global command parallelism cap + per-command timeout |
| `internal/discord/watchdog` | Gateway-silence detection and WS/ready tracking |
| `internal/command` | Command implementations (`play`, `next`, `stop`, `history`, `help`, `settings`, …) |
| `internal/config` | Env-driven config (`caarlos0/env` + `.env`); all runtime knobs live here |
| `internal/storage` + `internal/domain` | JSON datastore keyed by guild: command history, playback history, disabled commands |

External process dependencies: **ffmpeg** (required, every parser decodes through it) and
**yt-dlp** (optional, used by the `ytdlp-*` parsers). Binary paths default to `PATH` and can
be overridden via `ffmpeg.FFmpegPath` / `ytdlp.YtdlpPath`. `bwmarrin/discordgo` is replaced
with the vendored fork in `pkg/discordgo-fork-dev` (panic fixes, stream handling).

---

## The three extension layers

Everything pluggable sits behind one of three small interfaces:

```go
// pkg/music/sources — URL/query → track metadata (no stream URLs yet)
type Source interface {
    Match(input string) bool
    Resolve(input string, selectedParser string) ([]TrackInfo, error)
    SourceName() string
    AvailableParsers() []string
}

// pkg/music/parsers — track → PCM byte stream (s16le 48kHz stereo)
type Streamer interface {
    LinkStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
    PipeStream(track *TrackParse, seekSec float64) (io.ReadCloser, func(), error)
    SupportsPipe() bool
}

// pkg/music/sink — PCM stream → audio output
type AudioSink interface {
    Stream(stream io.ReadCloser, stop <-chan struct{}) error
}
type Provider interface {
    Sink(target string) (AudioSink, error)
    ReleaseSink(target string)   // player disconnected (leave VC)
    InvalidateSink()             // drop cached transport, next Sink() re-acquires
}
```

`TrackInfo` deliberately carries only a page URL, title, source name, and an ordered parser
preference list. Actual stream URLs are resolved lazily by the parser at open time, so
queued tracks never hold expiring CDN links.

---

## Resolution

`resolve.New()` registers the three sources. `Resolve(input, source, parser)` applies, in order:

1. **Explicit source selected** — validate the parser, then: bare query → allowed only for
   YouTube/SoundCloud (searchable sources); URL → must pass `Match`.
2. **Auto-detect, bare query** — always routed to YouTube.
3. **Auto-detect, URL** — deterministic precedence: YouTube, then SoundCloud (map iteration
   is never used for matching; a new source must be added to this list explicitly).
4. **Fallback** — radio, which validates the URL by probing its Content-Type.

Searching is scraping-based: YouTube search scrapes the results page with a regex;
SoundCloud search goes through DuckDuckGo HTML (`site:soundcloud.com`) because SoundCloud
has no keyless API. Both are the most fragile parts of the system by design — when they
break, only search breaks; direct URLs keep working.

---

## Playback pipeline

```mermaid
sequenceDiagram
  participant H as Slash handler
  participant P as player.Player
  participant RS as RecoveryStream
  participant S as AudioSink (Discord)
  H->>P: EnqueueTrackInfo(track)
  H->>P: PlayNext(voiceChannelID)
  P->>P: dequeue under playNextMu
  P->>RS: Open(0) — first working parser
  P->>P: spawn runPlayback goroutine
  P-->>H: nil (track started)
  H->>H: render "Now Playing" synchronously
  loop 20ms frames
    S->>RS: Read PCM
    S->>S: Opus encode, send (stop/timeout-guarded)
  end
  RS-->>S: EOF
  P->>P: completion goroutine → PlayNext
  alt queue non-empty
    P->>RS: next track (new goroutine)
    P->>P: emit StatusPlaying → watcher edits status message
  else queue empty
    P->>P: Stop(true) → ReleaseSink (leave VC)
    P->>P: watcher edits "Playback Finished"
  end
```

Key mechanics:

- **Queue** — a plain `[]TrackParse` under `p.mu`. `playNextMu` serializes dequeue+open so
  two tracks can never start concurrently.
- **Completion chain** — `runPlayback → completion goroutine → PlayNext → startTrack → new
  runPlayback`. Iteration happens via fresh goroutines, not recursion. Queue-end disconnect
  has a single decision point: `PlayNext` returning `ErrNoTracksInQueue` → `Stop(true)`.
- **Per-run ownership** — each run gets its own `stopPlayback`/`playbackDone` channels and
  its own track pointer. A stale run's goroutine can never clobber a newer run's state
  (`clearIfCurrent` compares track identity before resetting).
- **Discord sink** — reads fixed 20 ms frames (960 samples), warm-up of 10 frames, skips up
  to 150 leading silent frames, then Opus-encodes per frame. Every `OpusSend` is a `select`
  against the stop channel and a send timeout, so `Stop()` always unblocks the streaming
  goroutine and a stalled voice connection surfaces as `ErrVoiceTransport` instead of a hang.
- **Pause/Resume** — intentionally unsupported (the sink owns the read loop); commands get
  `ErrPauseNotSupported`.

### Status delivery (single-consumer contract)

`Player.PlayerStatus` is a buffered channel with **exactly one long-lived consumer per
player**. For the bot that consumer is `voice.Service.watchPlayerStatus`, spawned once when
the guild's player is created; it handles only *asynchronous* transitions (auto-advance →
edit "Now Playing", natural queue end → "Playback Finished"). Interaction-driven outcomes
("Now Playing" after `/play`, "Track(s) Added") are rendered synchronously by the handler,
which knows the result of `PlayNext` directly. Do not attach per-interaction listeners to
the channel — competing receivers steal events.

The guild status UI is a single message per guild (`voice.Service.UpdatePlaybackStatus`):
created via interaction followup on first use, edited thereafter — which is also why updates
keep working past the 15-minute interaction-token expiry.

---

## Failure handling

Three distinct failure classes, three distinct mechanisms:

1. **Media failures** (`stream.RecoveryStream`, per track):
   - *Instant fail* — error/EOF on the very first read → advance to the next parser in the
     track's preference list.
   - *Early EOF* — EOF before ~95 % of known duration → reopen the same parser at the
     current seek position (up to 3 attempts per parser), computed from bytes read.
   - Natural EOF passes through untouched.
2. **Voice transport failures** (`player.runPlayback`, up to 3 attempts): `ErrVoiceTransport`
   from the sink → `hard` mode invalidates the sink (forces a VC rejoin) or `soft` mode
   retries the stream first (`PLAYER_TRANSPORT_RECOVERY_MODE`, `PLAYER_TRANSPORT_SOFT_ATTEMPTS`),
   then reopens media at the current position without touching the media retry budget.
3. **Session failures** (`internal/discord`): a gateway-silence watchdog
   (`WS_SILENCE_TIMEOUT`) and a 30-second API probe (3 strikes) mark the session unhealthy;
   `DISCORD_UNHEALTHY_MODE` picks the reaction (`restart-session`, `restart-voice`, `ignore`).
   `main.go` runs `RunSession` in a restart loop; the **voice service outlives sessions**, so
   queues and players survive reconnects and sinks are simply invalidated and re-acquired.

User-facing error flow: synchronous failures are answered directly by the handler
(ephemeral embed). Asynchronous failures (a track dies mid-play) travel
`runPlayback → markPlaybackFailed → Options.OnPlaybackFailed → voice.Service.notifyPlaybackFailed`,
which edits the guild status message, falling back to a public message in the last-used
command channel. `internal/playbackerr` humanizes the raw error text.

`ProcessStream` (ffmpeg wrapper) converts a zero-byte EOF from a failed process into the
real process error, so an instant ffmpeg failure (403, bad URL) is never mistaken for a
clean track end. All parsers build their ffmpeg invocation through `ffmpeg.NewPCMCommand`,
which also captures and classifies ffmpeg stderr (403/forbidden/conversion failures at Warn).

---

## Discord command layer

Commands implement the melodix `Handler` interface and are registered through
`cmdadapter.Register` into `keshon/command`'s `DefaultRegistry`, wrapped in middleware
(guild-only, per-guild disabled-command gate, permission check, invocation logging).
Optional capabilities are discovered by interface assertion: `SlashProvider`,
`ContextMenuProvider`, `ComponentInteractionHandler`.

- **Dispatch** — `onInteractionCreate` routes slash/context-menu commands through
  `execguard` (parallelism cap `COMMAND_PARALLELISM`, timeout `COMMAND_TIMEOUT`); message
  components are matched by `customID` prefix convention (`name`, `name:`, `name_`).
- **Slash sync** — `cmdsync.Syncer` diffs desired vs. existing per-guild commands by
  name+type+fingerprint when `INIT_SLASH_COMMANDS=true`.
- **README generation** — `go run ./cmd/discord -readme` regenerates the command listing in
  `README.md` from the registry (dev step, run from the repo root; the bot never writes
  files at runtime).

Caveat: the `source`/`parser` choice lists in `/play`'s slash definition
(`internal/command/music/play/play.go`) are maintained by hand and must be kept in sync
with the resolver and `stream.Registry`.

---

## State & persistence

- **In-memory, per guild, survives reconnects** — `voice.Service`: players, sink providers,
  status-message ids, notify channels.
- **In-memory, per session** — `Bot`'s session context and exec guard (swapped atomically on
  each `RunSession`).
- **Disk** — a single JSON datastore (`STORAGE_PATH`, `keshon/datastore`) keyed by guild:
  disabled commands, command history (last 50), playback history (last 750, monotonic ids —
  `/play <id>` replays an entry without re-resolving).

Only tracks that actually start playing are recorded, via the `PlaybackRecorder` hook.

---

## Adding a new source or parser

**Source** (metadata only, reuses existing parsers):
1. New package under `pkg/music/sources/<name>/` implementing `Source`.
2. Add the name constant to `pkg/music/sources/sources.go`.
3. Register it in `resolve.New()` **and** add it to the auto-detect precedence list in
   `Resolver.Resolve` (deliberately explicit).
4. If searchable by bare query, extend the query branch in the resolver.
5. Add it to `/play`'s `source` choices.

**Parser** (new playback backend):
1. New package under `pkg/music/parsers/<name>/` implementing `Streamer` — build the ffmpeg
   stage with `ffmpeg.NewPCMCommand`, wrap the process in `ffmpeg.ProcessStream`, and make
   `cleanup` call `ProcessStream.Close()`.
2. Register it in `stream.Registry` (`pkg/music/stream/stream.go`); the `-pipe`/`-link` name
   suffix selects `PipeStream` vs `LinkStream`.
3. List it in the owning source's `AvailableParsers()` and `/play`'s `parser` choices.

The player, queue, recovery, sinks, and persistence are source- and parser-agnostic —
nothing else needs touching.

---

## Testing & verification

- `go test -race ./...` — the race detector is non-negotiable here; the player tests
  (`pkg/music/player/player_test.go`) include a concurrent hammer specifically to catch
  locking regressions. Fakes swap `stream.Registry` (same pattern as
  `pkg/music/stream/recovery_test.go`) and stub the sink provider.
- `internal/discord/voice/sink/sink_discord_test.go` pins the Opus-send contract: stop
  unblocks a stalled send; stall/closed channel → `ErrVoiceTransport`.
- Manual smoke checklist (needs a real guild): `/play` multi-track batch — the status
  message must update on every auto-advance; `/play` while playing → "Track(s) Added";
  `/next`; `/stop` mid-track returns promptly; natural queue end → single VC disconnect and
  "Playback Finished"; one `/play` per parser override.
- `cmd/cli` exercises the whole engine minus Discord: `go run ./cmd/cli`, then
  `play <url>`, `next`, `stop`, `queue`, `status`.
