# Conventions

These are the house rules. Most are enforced by tooling (gofmt, go vet,
staticcheck via golangci-lint, `go test -race`); the rest by review. If a rule
gets in the way of something that genuinely needs doing, pragmatism wins —
just leave a note in the code explaining the exception.

## Design principles

Go stays minimal here. No frameworks, no speculative abstraction — an
interface only exists if it has two real implementations or a real test seam
behind it. Everything else stays concrete.

There are exactly three extension points, and new capability should show up
as an implementation of one of them rather than as a new layer:
`sources.Source` (input → metadata), `parsers.Streamer` (track → Opus
packets), and `sink.AudioSink`/`Provider` (Opus packets → audio). The whole
engine speaks 20ms Opus packets (`opus.Reader`) end to end. See
[architecture.md](architecture.md) for how these fit together.

`pkg/music` never imports Discord. The CLI is proof this holds — it's the
same engine, no Discord in sight — and it should stay that way. Anything
Discord-specific belongs in `internal/`.

Parsers are expendable by design. They're expected to fail fast with a clear
error, and `RecoveryStream`'s fallback chain is what actually provides
reliability. Don't add retry logic inside a parser — recovery already covers
that from outside.

And no signature deciphering, ever. If a platform requires it, the track
falls through to kkdai or yt-dlp instead. That's a deliberate boundary, not
something waiting to be fixed later.

## Naming

The playback entity is `parsers.Track`; whatever the resolver produces is
`sources.TrackInfo`. Please don't add a third track-shaped type.

Package names are single lowercase words describing what the package does
(`reply`, `perm`, `watchdog`). A package that just wraps one dependency can be
named after it (`kkdai`, in the `goja` style) — that's just being honest
about what it is, not laziness.

Strings that select behavior get a named type plus constants (see
`player.TransportRecoveryMode`); identifier strings get constants too
(`sources.Parser*`, `sources.YouTube`). Raw string literals for either are
worth flagging in review.

## Frozen identifiers

Parser registry keys (`ytnative-link`, `kkdai-pipe`, and so on) and source
names (`youtube`, etc.) get persisted in guild playback history and shown as
slash-command choices, so they can't just be renamed later — add new ones
instead of touching existing ones. The constants live in
`pkg/music/sources/parsers.go` and `sources.go`; the registry mapping lives in
`pkg/music/stream/stream.go`, where `stream.Entry` decides link vs. pipe
dispatch.

## Concurrency contracts

`Player.PlayerStatus` is meant to have exactly one long-lived consumer per
player — the voice service's `watchPlayerStatus`, or the CLI's own loop.
Don't attach per-interaction listeners to it; competing receivers will steal
events from each other. Interaction outcomes should instead be rendered
synchronously by the handler that already knows the result.

Callback fields on `Player` are set once, at construction, via
`player.Options`, and never touched again after that.

Every goroutine has an owner and a clear way to exit. Per-run channels
(`stopPlayback`/`playbackDone`) belong to exactly one playback run, and a run
should identify its own state by its own `*parsers.Track` pointer
(`clearIfCurrent`) rather than by reading anything shared.

Package-level loggers use `atomic.Pointer[zerolog.Logger]` with `SetLogger`
and a `Nop` fallback (see `parsers/ffmpeg/pcm.go` for the pattern), wired once
in `internal/discord/session_bootstrap.go`.

## Errors and logging

Library errors carry the package name as a prefix, like `ytnative: player
request: …`. Sentinel errors (`ErrCipherOnly`, `ErrNoTracksInQueue`) are
exported and matched with `errors.Is` — the error text itself is for display
only and should never be pattern-matched against.

Log events are lowercase, snake_case, verb-last (`playback_running`,
`stream_open_failed`), with structured fields rather than interpolated
strings.

External process stderr should go through `ffmpeg.NewPCMCommand`'s
classifier rather than straight to the process's own stderr.

## Adding things

To add a source: implement `sources.Source` under
`pkg/music/sources/<name>/`, add the name constant to `sources/sources.go`,
register it in `resolve.New()` and add it to the auto-detect precedence list
in `Resolver.Resolve`. If it's searchable by bare query, extend that branch
too, and add it to `/play`'s source choices.

To add a parser: implement `parsers.Streamer.Open` returning an
`opus.Reader`, under `pkg/music/parsers/<name>/` — use `opus.Demux` if the
source is a native Opus container, otherwise go through ffmpeg via
`ffmpeg.NewPCMCommand` wrapped in `ffmpeg.OpusReader`. Add the key constant to
`sources/parsers.go`, add the instance to `stream.registryEntries`, and list
it in the owning source's `AvailableParsers()` plus `/play`'s parser choices.
If it talks to a live endpoint, add an opt-in live test
(`MELODIX_LIVE_TESTS=1`) so drift gets caught early.

## Testing & verification

`go test -race ./...` is the bar to clear — the race detector isn't optional
for anything that touches `Player`.

Prefer fakes over mocks: swap the registry via `stream.SetRegistry`, stub
`sink.Provider`, and use httptest for HTTP clients (base URLs are struct
fields specifically so this works).

Live-endpoint behavior only gets opt-in `Live` tests, never unconditional
ones.

Before cutting a release, run through the manual matrix in
[architecture.md](architecture.md#testing--verification): multi-track
auto-advance, `/stop` mid-track, a natural queue end, and one `/play` per
parser override.

## Formatting & CI

Code should be gofmt-clean and `go vet`-clean, and the `.golangci.yml` set
should pass with zero findings — it's kept deliberately curated so that a
finding actually means something when it shows up.

CI (`.github/workflows/build.yml`) runs vet, race tests, and lint on every
push and PR, then cross-compiles all release targets.

`README.md` is generated, not hand-edited: change `README.md.tmpl` and run
`go run ./cmd/discord -readme` from the repo root. The bot itself never
writes files at runtime.

## Release notes

Release notes are read by people deciding whether to run this bot, not by
whoever fixed the bug. Lead with what changed *for them*, in plain language —
"YouTube plays again, and now without ffmpeg" — and say whether upgrading
costs them anything: config changes, a migration, a new dependency. One short
paragraph of context is plenty.

Root-cause detail is worth keeping, just not at the top. Put it in a collapsed
`<details>` block at the end, or leave it in the commit messages, where the
next maintainer looks anyway. Parser keys, InnerTube client names, HTTP header
syntax and internal log-event names mean nothing to someone choosing a music
bot, and a release page that opens with them reads as a changelog for the
author rather than news for the reader. Commit messages are the opposite case
and stay as technical as they need to be.

The release body comes from the **annotated tag's message body** —
`.github/workflows/release.yml` reads `%(contents:body)` — so that is where
the notes get written, and it has one sharp edge:

```bash
git tag -a --cleanup=verbatim vYYYY.MM.DD -F notes.md
```

`--cleanup=verbatim` is not optional. Git's default cleanup strips every line
beginning with `#` as a comment, which silently eats Markdown headings and
leaves a published release with its structure missing. The tag's subject line
is not part of the release body either, so nothing load-bearing goes there.
