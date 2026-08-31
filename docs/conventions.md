# Conventions

These are the house rules.

## How to read this

Every rule below carries a tier, because a rule you cannot verify is worse
than no rule: it creates the belief of compliance without the fact, and it
teaches readers that the rules around it are optional too.

- **[enforced: <name>]** — a check fails, and `<name>` says which one.
`internal/conventions` is an ordinary Go test, so `go test ./...` and CI run
it with everything else; `.golangci.yml`, `gofmt` and the race detector
cover the tags they own. `TestDocumentAndChecksAgree` keeps these tags and
those checks in step, so this file cannot advertise enforcement that does
not run, and a check cannot enforce something this file never mentions. The
wording of each enforced rule is read back out of this document for the
failure message, so the sentence you are reading is the one the build quotes
at you.
- **[invariant]** — nothing checks it, and violating it breaks something
  nameable, often silently and often for users. These are the expensive ones.
  Each states the failure it prevents, so the cost is on the page rather than
  in someone's memory.
- **[practice]** — how things are done here. Nothing breaks if you deviate;
  the codebase just gets less coherent. Deliberately few.

A rule earns a place here only if it can claim one of those three. A
requirement without a tolerance and a test method is a preference, and
preferences get deleted rather than documented — that filter is what keeps
this file short enough to be read.

If a rule gets in the way of something that genuinely needs doing, pragmatism
wins — take the exception and leave a note in the code explaining it.
`parsers/ytnative/chunked.go`'s `fetchChunk` is the worked example: it retries
inside a parser, which the design principles forbid, and says in place why the
usual answer is wrong there.

**This file is executable.** `internal/conventions` reads it while the tests
run, so editing it is a code change, not a documentation change. Reword a
paragraph tagged `[enforced: x]` and you have reworded a build failure
message. Remove the tag, rename it, or move this file, and the build goes
red until the checks are updated to match — deliberately, because a rule
that quietly stops being enforced is exactly the failure this whole
arrangement exists to prevent. Prose under `[invariant]` and `[practice]` is
free to change; nothing reads it.

**Adding a rule.** Give it a tier. If it would be [enforced], write the check
in `internal/conventions/conventions_test.go` first and let it fail, then
record the baseline. If it would be [invariant], name the failure in the rule
itself. If it is neither, do not add it.

## Design principles

**[enforced: discord-free]** `pkg/music` never imports Discord. The CLI is
the proof it holds — same engine, no Discord in sight. Anything
Discord-specific belongs in `internal/`. Checked by
`TestLibraryStaysDiscordFree`.

**[practice]** Go stays minimal here. No frameworks, no speculative
abstraction — an interface only exists if it has two real implementations or a
real test seam behind it. Everything else stays concrete.

**[practice]** New capability shows up as an implementation of one of the
three extension points rather than as a new layer: `sources.Source`
(input → metadata), `parsers.Streamer` (track → Opus packets), and
`sink.AudioSink`/`Provider` (Opus packets → audio). `sources.Searcher` is an
optional extra a source may also implement, and is deliberately not folded
into `Source`: radio has nothing to rank, and resolving a query to one track
is a different job from listing candidates. The whole engine speaks 20ms Opus
packets (`opus.Reader`) end to end. See
[architecture.md](architecture.md#the-three-extension-layers).

**[invariant]** Parsers are expendable by design. They fail fast with a clear
error, and `RecoveryStream`'s fallback chain is what provides reliability.
Don't add retry logic inside a parser: recovery already covers it from
outside, and a parser that retries turns one failed source into a stall the
chain cannot route around. The two deliberate exceptions both live in
`ytnative` and both say so in place.

**[invariant]** No signature deciphering, ever. If a platform requires it, the
track falls through to kkdai or yt-dlp instead. That boundary is what keeps
this project out of the cat-and-mouse game that eats the maintenance budget of
every tool that took the other road.

## Naming

**[practice]** The playback entity is `parsers.Track`; whatever the resolver
produces is `sources.TrackInfo`. Please don't add a third track-shaped type.

**[practice]** Package names are single lowercase words describing what the
package does (`reply`, `perm`, `watchdog`). A package that just wraps one
dependency can be named after it (`kkdai`, in the `goja` style) — that's being
honest about what it is, not laziness.

**[practice]** Strings that select behavior get a named type plus constants
(see `player.TransportRecoveryMode`); identifier strings get constants too
(`sources.Parser*`, `sources.YouTube`).

## Frozen identifiers

**[enforced: frozen-identifiers]** Parser registry keys (`ytnative-link`,
`kkdai-pipe`, and so on) and source names (`youtube`, etc.) are persisted in
guild playback history and
registered as slash-command choices, so they cannot be renamed later — add new
ones instead of touching existing ones. The constants live in
`pkg/music/sources/parsers.go` and `sources.go`; the registry mapping lives in
`pkg/music/stream/stream.go`. `TestFrozenIdentifiers` pins every value and has
no baseline, because there is nothing to ratchet toward.

**[enforced: frozen-identifiers]** `/search`'s button ids are frozen for a
different reason but just as hard. The format is
`search:<source>:<payload>`, and a chooser that has already been posted
keeps sitting in a channel: its ids come back whenever someone presses a
button, possibly long after a restart or a deploy. So the source tags (`yt`,
`sc` in `internal/command/music/search`) can gain new values but must never
be renamed or re-pointed. Pinned by the same test.

**[invariant]** An unrecognised tag has to fail closed — telling the user to
run `/search` again — rather than resolve as some default source. A tag that
silently resolves elsewhere plays the wrong track from a button the user
pressed in good faith.

## Concurrency contracts

**[invariant]** `Player.PlayerStatus` has exactly one long-lived consumer per
player — the voice service's `watchPlayerStatus`, or the CLI's own loop. Don't
attach per-interaction listeners: competing receivers steal events from each
other, so a status update simply goes missing rather than failing loudly.
Interaction outcomes get rendered synchronously by the handler that already
knows the result.

**[invariant]** Callback fields on `Player` are set once, at construction, via
`player.Options`, and never touched again. They are read from the playback
goroutine without a lock, which is only safe because of that.

**[invariant]** Every goroutine has an owner and a clear way to exit.
Per-run channels (`stopPlayback`/`playbackDone`) belong to exactly one
playback run, and a run identifies its own state by its own `*parsers.Track`
pointer (`clearIfCurrent`) rather than by reading anything shared — otherwise
a goroutine scheduled late acts on a newer run's track.

**[practice]** Package-level loggers use `atomic.Pointer[zerolog.Logger]` with
`SetLogger` and a `Nop` fallback (see `parsers/ffmpeg/pcm.go`), wired once in
`internal/discord/session_bootstrap.go`.

## Errors and logging

**[enforced: error-prefix]** Errors in `pkg/music` carry the package name as a
prefix, like `ytnative: player request: …`. Exported sentinels are exempt:
their text is also the string a user is shown, and `ytnative: no tracks in
queue` is not a sentence anyone wants in a Discord embed. The rule covers
wrapped and internal errors — everything that exists to be read in a log.

**[invariant]** Sentinel errors (`ErrCipherOnly`, `ErrNoTracksInQueue`) are
exported and matched with `errors.Is`. Never pattern-match the error text: it
is display copy, it gets reworded, and a match against it fails silently when
that happens.

**[enforced: log-event-naming]** Log events are lowercase, snake_case,
verb-last (`playback_running`, `stream_open_failed`), with structured fields
rather than
interpolated strings. `Msgf` is a violation for the same reason — an
interpolated message cannot be grepped or aggregated.

**[practice]** External process stderr goes through `ffmpeg.NewPCMCommand`'s
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

**[invariant]** Four things make such a comment hold up:

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

**[invariant] One fact, one home.** The four qualities above say what makes a
comment worth writing; they do not license writing it four times. A fact told
in two places will disagree with itself within a release, and the disagreement
is silent — both copies read as authoritative. When two comments would explain
the same decision, the one at the point of decision keeps it and the other
points there by name. This is the rule that bites hardest right after a design
is worked out, when the reasoning feels worth repeating everywhere it touches:
that is exactly when to write it once.

There is no threshold on how much a file may explain, and there should not be
— `resume.go` and `chunked.go` are mostly prose because the decisions behind
them are genuinely unobvious. What the scorecard in `internal/conventions`
prints is comment density per file, not as a gate but so drift is visible.
Read the top of that list occasionally and ask whether those files are
explaining hard things or repeating themselves.

Long explanations belong in one prose block at the top of the file or above
the declaration they explain — `parsers/ytnative/resume.go`,
`parsers/ytnative/chunked.go`, `parsers/ytdlp/runtime.go` and
`sources/youtube/playlist.go` are the pattern — rather than sprinkled line by
line through the body. Struct fields carrying a contract get their own
comment, and concurrency-relevant ones say who owns the field and what lock
covers it ("belongs to the reading goroutine alone", "never held across a read
or an HTTP round trip").

**[practice]** Every exported identifier gets a doc comment starting with its
own name, per Go convention — except methods implementing an interface, which
inherit the interface's doc and should not repeat it. `revive`'s exported-docs
linter is deliberately off: `pkg/music` is documented by hand and `internal/`
favors self-explanatory names over comment ceremony.

**[enforced: comment-width]** Comments wrap at 80 columns and are written in
English. A tab counts as one column, and a line carrying an unbreakable
token (a URL, a long identifier) is exempt.

**[enforced: file-headers]** No file-path headers (`// FILE: internal/…`).
Nothing checks them, so they survive every rename — server-domme carried one
naming `melodix/internal/discord/middleware/command_logger.go`, a path that has
never existed in either project, and it had been wrong since the commit that
introduced it.

**[invariant]** Two more things go stale silently, so they don't get written at
all: a claim about what does not exist yet ("only YouTube is
offered today"), which still reads as fact long after it stopped being one —
describe what the design allows instead; and any restatement of a constant's
value, which the constant already carries. A comment that has drifted from the
code is worse than no comment, because it is believed.

## Adding things

The step-by-step checklists for a new source, a new parser, and a new
`/search` integration live in
[architecture.md](architecture.md#adding-a-new-source-or-parser), next to the
package map they refer to. What this file adds are the obligations that come
with them:

**[enforced: frozen-identifiers]** A new parser key or source name is a frozen
identifier from the moment it ships, and the check derives the set from the
source rather than a list: add an exported string constant to a frozen
package and the build fails until it is pinned in `frozenPackages`. That is
deliberate friction — it forces the question "is this string persisted?" at
the moment the answer is still cheap.

**[invariant]** A `SearchResult` `ID` is the source's own compact identifier,
never a URL. It has to survive a round trip through a Discord component id,
which caps at 100 characters, and SoundCloud permalinks routinely exceed the
budget that leaves. `trackURL` is what turns that id back into a page URL.

**[invariant]** A parser that talks to a live endpoint gets an opt-in live
test (`MELODIX_LIVE_TESTS=1`) so upstream drift is caught early, and an
entry-point test that does not need the network — see below for why both.

## Testing & verification

**[enforced: race]** `go test -race ./...` is the bar to clear; the race
detector isn't optional for anything that touches `Player`. CI runs it on
every push.

**[practice]** Prefer fakes over mocks: swap the registry via
`stream.SetRegistry`, stub `sink.Provider`, and use httptest for HTTP clients.
Endpoint and tuning values are `var` rather than `const` specifically so tests
can point them somewhere else (`playerEndpoint`, `homeEndpoint`, `chunkSize`).

**[invariant]** Live-endpoint behavior only gets opt-in `Live` tests, never
unconditional ones. A network call in the default suite makes CI fail for
reasons that have nothing to do with the commit, and the usual response is to
stop trusting the suite.

**[invariant]** Test the entry point, not only the helpers underneath it. A
parser's `Open` is the thing the engine actually calls, so it needs its own
coverage even when every piece it composes is already tested — a check sitting
between the helpers and the caller otherwise belongs to no test at all. That
is not hypothetical: a live-stream gate in `ytnative`'s `Open` rejected every
playable YouTube video for a whole release while both the unit tests and the
opt-in live tests stayed green, because all of them called `fetchPlayer` and
`pickOpusFormat` directly and nothing called `Open`.

**[invariant]** A regression test has to be watched failing before it is
trusted. Reintroduce the bug, confirm the new test goes red, then take it back
out. A test that passes for a reason other than the one you intended is the
normal outcome, not the rare one: `chunked.go`'s cache-corruption guard was
written, reviewed, and green — against a mutation of the exact bug it existed
to catch — because it exercised a neighbouring code path. Only the mutation
found that.

**[practice]** Before cutting a release, run the manual matrix in
[architecture.md](architecture.md#testing--verification).

## Formatting & CI

**[enforced: golangci]** Code is gofmt-clean and `go vet`-clean, and
`.golangci.yml` passes with zero findings — the set is kept deliberately
curated so a finding always means something. `internal/conventions` runs as
part of the same `go test ./...`.

The convention checks ratchet: `internal/conventions/baseline.json` records
what each file owed when a rule was introduced, and a rule fails only when a
file gets **worse**. A new file carries no allowance, so it meets every rule
in full; a file that already owes something is only required not to owe more.
That is what lets a rule be adopted on a live codebase without a repo-wide
edit nobody can review.

**The baseline is currently empty.** All three ratcheted rules hold
everywhere, so every violation is a real regression rather than a number
creeping up — which is the state worth defending. It got there by burning the
debt down (263 violations, in one pass) rather than by lowering the bar, and
`git log` has the commit if the method is ever needed again.

Two things to know if debt ever returns. It is a per-file **count**, not a
per-line record: inside a file that has an allowance you could remove one
violation and add another without the build noticing. And a `git mv` moves a
file out from under its allowance, which reads as a regression until the
baseline is re-accepted. Line-exact ratcheting is available off the shelf —
`golangci-lint run --new-from-merge-base=origin/main` — if either trade stops
being acceptable.

After fixing violations, lock the gain in:

```bash
CONVENTIONS_UPDATE=1 go test ./internal/conventions/
```

On Windows, `conventions.bat` runs the checks and prints the scorecard, and
`conventions.bat accept` does the line above. `check.bat` runs the whole gate —
gofmt, vet, lint, race tests — which is what CI does.

An update run **always fails**, on purpose: it skips the ratchet, so a green
exit would mean one stray environment variable — a shell profile, a CI block,
an agent's environment — silently disabling every ratcheted rule with nothing
to show for it. Re-run without the variable to verify, and read the diff to
`baseline.json` before committing it. The number going up is the finding; the
baseline is only bookkeeping.

**[practice]** CI (`.github/workflows/build.yml`) runs vet, race tests, and
lint on every push and PR, then cross-compiles all release targets.

**[invariant]** `README.md` is generated, not hand-edited: change
`README.md.tmpl` and run `go run ./cmd/discord -readme` from the repo root.
Editing the output means losing the edit on the next regeneration. The docs
under `docs/` and `pkg/music/README.md` are hand-written. The bot itself never
writes files at runtime.

## Release notes

**[practice]** Release notes are read by people deciding whether to run this
bot, not by whoever fixed the bug. Lead with what changed *for them*, in plain
language — "YouTube plays again, and now without ffmpeg" — and say whether
upgrading costs them anything: config changes, a migration, a new dependency.
One short paragraph of context is plenty.

**[practice]** Root-cause detail does not go on the release page at all — not
at the top, and not folded into a `<details>` block at the end either. It
lives in the commit messages, where the next maintainer actually looks. Parser
keys, InnerTube client names, HTTP header syntax and internal log-event names
mean nothing to someone choosing a music bot. Commit messages are the opposite
case and stay as technical as they need to be, which is what makes leaving the
detail out here cost nothing.

**[invariant]** The release body comes from the **annotated tag's message
body** — `.github/workflows/release.yml` reads `%(contents:body)` — so that is
where the notes get written:

```bash
git tag -a --cleanup=verbatim vYYYY.MM.DD -F notes.md
```

`--cleanup=verbatim` is not optional. Git's default cleanup strips every line
beginning with `#` as a comment, which silently eats Markdown headings and
publishes a release with its structure missing. The tag's subject line is not
part of the release body either, so nothing load-bearing goes there.
