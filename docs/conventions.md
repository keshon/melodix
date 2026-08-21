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
packets), and `sink.AudioSink`/`Provider` (Opus packets → audio).
`sources.Searcher` is an optional extra a source may also implement, and is
deliberately not folded into `Source`: radio has nothing to rank, and
resolving a query to one track is a different job from listing candidates. The whole
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

`/search`'s button ids are frozen for a different reason but just as hard.
The format is `search:<source>:<payload>`, and a chooser that has already been
posted keeps sitting in a channel: its ids come back whenever someone presses
a button, possibly long after a restart or a deploy. So the source tags (`yt`,
`sc` in `internal/command/music/search`) can gain new values but must never be
renamed or re-pointed, and an unrecognised tag has to fail closed — telling
the user to run `/search` again — rather than resolve as some default source.

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

## Comments

A comment earns its place by saying something the code cannot. The code
already says what it happens to do; a restatement is just a second thing to
keep in sync, and it sits in the way of the comment that actually matters.
`// Create embed` above a struct literal called `embed` is the shape to avoid,
and so is a doc comment that only expands the identifier back into a sentence.

The comments worth writing answer a question the code raises but cannot
settle. Why VISIONOS rather than the library's default client. Why the
read-ahead buffer sits above recovery instead of under it. Why a run compares
its own `*parsers.Track` pointer instead of reading shared state. Whoever asks
those next — a maintainer months from now, or an agent told to "clean this
up" — cannot recover the answer from the code, and will helpfully undo it.

Four things make such a comment hold up:

**Say whether it was measured or assumed.** "Verified against the live CDN,
not inferred", or "measured on one live broadcast, the renditions ran 269,
507, 962, 1282, 2922 and 5552 kbit/s", is worth more than any amount of
confident prose: it tells a reader which claims they may reason from and which
they should re-check. A guess is fine to write down — label it as one.

**Name the failure it prevents.** "which killed every playback on its first
read" turns an arbitrary-looking constant into something nobody deletes by
accident. A rule with no consequence attached reads as a preference.

**Say what not to do.** `don't reach for it when a 403 turns up`, `do not
switch to CBR`, `do NOT advance parserIndex`. A deliberate non-obvious choice
needs a fence around it or it gets optimized away — this is the highest-value
kind of comment here, and the one an agent is likeliest to violate in its
absence.

**Point at the next hop by name.** `see ytnative/visitor.go`, `see
VisionOSClient`, `see the note in SetCommand`. A reader who needs more should
be told where it is, in a form that greps.

Long explanations belong in one prose block at the top of the file or above
the declaration they explain — `parsers/ytnative/resume.go`,
`parsers/ytdlp/runtime.go` and `sources/youtube/playlist.go` are the pattern —
rather than sprinkled line by line through the body. Struct fields carrying a
contract get their own comment, and concurrency-relevant ones say who owns the
field and what lock covers it ("belongs to the reading goroutine alone",
"never held across a read or an HTTP round trip").

On mechanics: every exported identifier gets a doc comment starting with its
own name, per Go convention — except methods implementing an interface, which
inherit the interface's doc and should not repeat it. Comments wrap at 80
columns and are written in English.

Three things go stale silently, so they don't get written at all: file-path
headers (`// FILE: internal/…`), which nothing checks and which outlive a `git
mv`; a claim about what does not exist yet ("only YouTube is offered today"),
which still reads as fact long after it stopped being one — describe what the
design allows instead; and any restatement of a constant's value, which the
constant already carries. A comment that has drifted from the code is worse
than no comment, because it is believed.

## Adding things

To add a source: implement `sources.Source` under
`pkg/music/sources/<name>/`, add the name constant to `sources/sources.go`,
register it in `resolve.New()` and add it to the auto-detect precedence list
in `Resolver.Resolve`. If it's searchable by bare query, extend that branch
too, and add it to `/play`'s source choices.

To make a source searchable in `/search`: implement `sources.Searcher`
(`Search(query, limit) ([]SearchResult, error)`) on the source's searcher
type, and add a case to `/search`'s `pick` and `trackURL`. The `SearchResult`
`ID` must be the source's own compact identifier, never a URL — it has to
survive a round trip through a Discord component id, which caps at 100
characters, and SoundCloud permalinks routinely exceed the budget that
leaves. `trackURL` is what turns that id back into a page URL, with a lookup
if the source needs one.

The source tags in those button ids (`yt`, `sc`) are frozen the same way
parser keys are, and for the same reason: choosers already posted keep
sitting in channels and their ids come back when someone presses a button.
Add tags, never rename them.

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
