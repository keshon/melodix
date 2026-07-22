# Conventions

The house rules. Everything here is enforced either by tooling (gofmt, go vet,
staticcheck via golangci-lint, `go test -race`) or by review discipline. When a
rule and pragmatism conflict, pragmatism wins — but note the exception in code.

## Design principles

- **Minimal Go.** No frameworks, no speculative abstraction. An interface earns
  its existence by having two real implementations or a real test seam.
  Everything else is concrete.
- **Three extension layers, nothing else.** `sources.Source` (input → metadata),
  `parsers.Streamer` (track → Opus packets), `sink.AudioSink`/`Provider` (Opus
  packets → audio). The engine's currency is 20ms Opus packets (`opus.Reader`) end
  to end. New capability should arrive as an implementation of one of these, not as
  a new layer. See [architecture.md](architecture.md).
- **The engine (`pkg/music`) never imports Discord.** The CLI existing proves
  it; keep it that way. Discord-specific behavior lives in `internal/`.
- **Parsers are expendable.** They fail fast with a clear error; the
  `RecoveryStream` fallback chain is the reliability mechanism. Never add
  retries inside a parser that recovery already provides outside it.
- **No signature deciphering, ever.** When a platform requires it, that track
  falls through to kkdai/yt-dlp. This is a load-bearing decision, not a TODO.

## Naming

- The playback entity is `parsers.Track`; the resolver's product is
  `sources.TrackInfo`. Don't introduce a third track-ish type.
- Package names are single lowercase words describing the role (`reply`,
  `perm`, `watchdog`). A package wrapping one dependency may be named after it
  (`kkdai`, `goja`-style) — that's honest, not lazy.
- Behavior-selecting strings get a named type + constants
  (`player.TransportRecoveryMode`); identifier strings get constants
  (`sources.Parser*`, `sources.YouTube`). Raw string literals for either are a
  review flag.

## Frozen identifiers

Parser registry keys (`ytnative-link`, `kkdai-pipe`, …) and source names
(`youtube`, …) are **persisted in guild playback history** and shown as slash
choices. Never rename an existing key — only add new ones. The constants live
in `pkg/music/sources/parsers.go` and `sources.go`; the registry mapping lives
in `pkg/music/stream/stream.go` (`stream.Entry` decides link vs pipe dispatch).

## Concurrency contracts

- `Player.PlayerStatus` has **exactly one long-lived consumer** per player
  (the voice service's `watchPlayerStatus`, or the CLI loop). Never attach
  per-interaction listeners; competing receivers steal events. Interaction
  outcomes are rendered synchronously by the handler that knows them.
- Callback fields on `Player` are set once at construction via
  `player.Options` and never mutated afterwards.
- Every goroutine has an owner and an exit condition; per-run channels
  (`stopPlayback`/`playbackDone`) belong to one playback run only, and a run
  identifies its state by its own `*parsers.Track` pointer (`clearIfCurrent`),
  never by reading shared fields.
- Package-level loggers use `atomic.Pointer[zerolog.Logger]` + `SetLogger` +
  a `Nop` fallback (see `parsers/ffmpeg/pcm.go`); wired once in
  `internal/discord/session_bootstrap.go`.

## Errors and logging

- Library errors are prefixed with the package name: `ytnative: player
  request: …`. Sentinel errors (`ErrCipherOnly`, `ErrNoTracksInQueue`) are
  exported and matched with `errors.Is`; error *text* is display-only and never
  pattern-matched.
- Log events are lowercase snake_case verbs-last (`playback_running`,
  `stream_open_failed`), with structured fields, never interpolated messages.
- External process stderr goes through `ffmpeg.NewPCMCommand`'s classifier,
  not raw to the process stderr.

## Adding things

**A source** — implement `sources.Source` in `pkg/music/sources/<name>/`; add
the name constant to `sources/sources.go`; register in `resolve.New()` *and*
the auto-detect precedence list in `Resolver.Resolve`; extend the bare-query
branch if searchable; add the `/play` source choice.

**A parser** — implement `parsers.Streamer.Open` returning an `opus.Reader` in
`pkg/music/parsers/<name>/` (native Opus container → `opus.Demux`; otherwise
ffmpeg via `ffmpeg.NewPCMCommand` wrapped in `ffmpeg.OpusReader`); add the key
constant to `sources/parsers.go`; add the instance to `stream.registryEntries`;
list it in the owning source's `AvailableParsers()` and the `/play` parser
choices. If it talks to a live endpoint, add an opt-in live test
(`MELODIX_LIVE_TESTS=1`) as a drift canary.

## Testing & verification

- `go test -race ./...` is the bar; the race detector is non-negotiable for
  anything touching `Player`.
- Fakes over mocks: swap the registry via `stream.SetRegistry`, stub `sink.Provider`, httptest for
  HTTP clients (base URLs are struct fields for exactly this reason).
- Live-endpoint behavior gets opt-in `Live` tests, never unconditional ones.
- Before a release: the manual matrix in [architecture.md](architecture.md#testing--verification)
  (multi-track auto-advance, `/stop` mid-track, natural queue end, one `/play`
  per parser override).

## Formatting & CI

- gofmt-clean, `go vet`-clean, and the `.golangci.yml` set passes with zero
  findings — it's curated so a finding always means something.
- CI (`.github/workflows/build.yml`) runs vet + race tests + lint on every
  push/PR, then cross-compiles all release targets.
- `README.md` is generated: edit `README.md.tmpl` and run
  `go run ./cmd/discord -readme` from the repo root. The bot never writes
  files at runtime.
