# Ownership

Every bug the September 2026 audit found in the voice and playback layers was
one bug wearing nine costumes: **something wrote state it did not own, and
nothing noticed.**

A pointer handed to a renderer while a parser filled it in. A stream reopened
from the wrong goroutine. A lock held across a network round trip. A cached
connection whose owner had thrown it away. None of them broke a build, failed
a test, or looked wrong in review — and one of them silently removed
end-to-end encryption during a library migration.

Go will not catch this class. Rust refuses to compile it and Erlang makes it
unreachable across process boundaries; Go hands you a pointer and trusts you.
So the rules have to be written down, and — this is the part that matters —
**each one has to name what enforces it**, because a rule enforced only by
review is a rule that will be broken by the next person in a hurry, which is
exactly how these arrived.

## How to read the enforcement column

Strongest first. A rule's grade is the strongest thing that would stop a
change violating it:

| Grade | Means |
|---|---|
| **compiler** | The violation does not typecheck. The strongest grade available, and the only one that needs no vigilance. |
| **refused** | It compiles, and fails at runtime with an error naming the mistake. |
| **checked** | A test reads the source and fails on the shape. Cheap, and it survives refactors that tests of behaviour do not. |
| **tested** | A test fails, usually under `-race`. Depends on the test driving the right interleaving. |
| **documented** | A comment says so. Review is the only enforcement. Treat every one of these as a rule that will eventually be broken. |

`go test -race ./...` runs everything below.

## The rules

### 1. A Track leaves the engine by value

`Player.CurrentTrack` returns `parsers.Track`, not a pointer. Nothing exported
from the player hands out a `*parsers.Track`.

*Why:* the Track being played is written while it plays — the parser that
actually opened it, whether it is passthrough or cached, a title the media knew
and the resolver did not. The status watcher renders it from another goroutine.
Sharing the pointer was a live race on the default configuration, invisible to
`-race` because no test drove a parser switch and a render together.

*Enforcement:* **compiler** for the callers (a value cannot be written back
into the engine), plus **checked** —
`player.TestNoExportedMethodHandsOutATrackPointer` reads the signatures.

### 2. A stream owns its own Track; facts travel back as values

`RecoveryStream` copies the Track it is given. Parsers keep writing through the
pointer they are handed at open time, and it lands on the stream's copy.
What the stream learned returns as a `stream.OpenInfo` — from `Start`, and
through the confirmation callback — which the player applies under its own
lock.

*Why:* `Streamer.Open(track *Track, …)` is an out-parameter written by five
parser packages. That pointer used to be the player's `currTrack`.

*Enforcement:* **tested** —
`stream.TestRecoveryStreamDoesNotWriteTheCallersTrack` asserts the caller's
Track is byte-identical after a mid-track parser switch.

### 3. A stream's run state has exactly one writer, and it is whoever reads it

Everything a stream mutates — parser index, position, retry counts, cache
writer, its Track copy — belongs to the goroutine pulling packets. `Start`
opens once, before there is a reader. After that the only way in is
`RequestReopen`, which sets a flag and closes the source to unblock a parked
read; the reader performs the reopen itself.

*Why:* the previous `ReopenAfterTransportFailure` did the reopen from the
playback goroutine while the read-ahead producer was running. One of the fields
is a map, and the runtime does not survive a concurrent write to one. It had
never fired only because the error that reached it was itself unreachable.

*Enforcement:* **compiler** — `open` is unexported, so no other package can
call it at all; **refused** — a second `Start` returns an error rather than
performing it; **tested** —
`stream.TestRequestReopenDoesNotRaceTheReadAheadProducer` under `-race`.

There is deliberately no exported accessor for what a stream is playing.
Copying the answer out does not help: the read itself races the producer.

### 4. `p.mu` is never held across I/O

Capture what the call needs under the lock, release it, then make the call.

*Why:* the player's outward edges are a voice join (15s), a voice leave (10s)
and a stream open (a fetch or an ffmpeg process). Held across any of them, the
lock guarding one guild's queue is every reader of that queue's wait — and
before command bodies left the gateway goroutine, that was every guild's wait.

*Enforcement:* **checked** — `player.TestLockIsNeverHeldAcrossIO` scans
`player.go` for the outward interfaces' method names inside a locked region.
It is keyed on method names, not receivers, because the way to move a call out
from under a lock is to capture the receiver into a local first — so a check
keyed on receivers goes quiet exactly when somebody puts one back.

*Known limit, stated because a check that hides its blind spot is worse than
none:* it does not follow calls. A helper invoked under the lock that itself
does I/O passes this and is still wrong.

### 5. A run resets only its own state

Every playback run has a generation. `Stop`, `clearIfCurrent` and the
confirmation callback all compare against it before writing anything, and the
queue-end teardown names its own run rather than "whatever is playing".

*Why:* `Stop` waits up to ten seconds in the middle for the playback goroutine
to exit. At a track boundary the run that finishes during that wait is not the
run that was current when it started, so the reset landed on a track nobody
asked about — leaving audio playing that the player believed was not, with no
channel anyone could still signal.

*Enforcement:* **tested** — `player.TestStopDoesNotResetANewerRun`,
`TestASupersededRunDoesNotClearTheCurrentOne`,
`TestAQueueEndTeardownDoesNotStopTheTrackThatFollowedIt`, and the concurrent
hammer.

### 6. Nothing reachable from a Player has session lifetime

A `Player` outlives every gateway session. A `voicesink.Provider` therefore
holds no voice manager and no DAVE registry; it resolves both per acquisition
through a `Resources` function.

*Why:* a voice manager belongs to one `bot.Client` and closes over that
client's gateway. Held across a reconnect, the guild joins through a gateway
that has been shut — which fails instantly and permanently, and which no sink
invalidation fixes, because what went stale was the thing that makes
connections rather than a connection.

*Enforcement:* **checked** —
`voicesink.TestAProviderStoresNothingThatDiesWithTheSession` reflects over the
struct; **tested** — `TestAProviderUsesTheLiveSessionAfterARestart` swaps the
session underneath a live provider.

The tempting violation is "resolve once in the constructor and keep it". It
looks like a tidy-up and reads almost identically, which is why this one is
checked rather than documented.

### 7. Connection liveness is the voice manager's answer, never a field of ours

The provider keeps the connection it opened only to recognise it again, and
compares it by identity — never by reading protocol state off it, which disgo
writes from the gateway goroutine without a lock.

*Why:* disgo removes a connection on a voice websocket close it cannot resume
from, and on closing the client, and reports neither. Both the acquire and the
release path have to ask: acquiring without asking hands the next track a sink
over a dead socket, releasing without asking spends the close budget waiting on
a gateway that is not there.

*Enforcement:* **checked** —
`voicesink.TestEveryPathThatDecidesLivenessAsksTheManager`; **tested** —
`TestAConnRemovedByTheLibraryForcesARejoin`,
`TestReleasingAConnTheLibraryAlreadyTookDoesNotCloseIt`.

### 8. A sink never emits a frame its transport cannot protect

While the guild's DAVE session reports it has no live epoch, the frame provider
withholds frames — without consuming the packets it is holding, so the hold is
a pause rather than a gap. A hold that outlasts its budget ends the track as a
transport failure.

*Why:* dave-go forwards a frame unmodified rather than failing when no ratchet
is selectable, and disgo's send path never asks. This property existed in the
discordgo fork, in the fork's send loop, and the migration deleted the loop —
so it vanished with no compile error and no failing test.

*Enforcement:* **tested** — `voicesink.TestHoldStopsSendingWithoutAnEpoch` and
the three cases beside it; **documented** in `pkg/music/sink`'s `AudioSink`
contract, which is the thing whose absence let it vanish.

### 9. A sink detects its own transport dying

`Sink.Stream` watches two things it can see: the audio sender has stopped
asking for frames, and the connection it was built on is no longer the guild's.

*Why:* disgo offers exactly one signal for a connection going away under a
running track, `OpusFrameProvider.Close`, and v0.19.6 never calls it. So the
sink blocked forever — track never ended, queue never advanced, "Now Playing"
until someone typed `/stop` — and the player's entire transport recovery sat
behind an error nothing could produce.

*Enforcement:* **tested** — `voicesink.TestClosedSocketEndsTheTrackAsTransportFailure`
and `TestAConnRemovedBehindOurBackEndsTheTrack` run disgo's real audio sender
against a stub socket; **documented** in the `AudioSink` contract.

### 10. The gateway read loop dispatches and returns

Command bodies run on a per-guild worker. `Ready` and `GuildJoin` stay inline,
because their ordering is load-bearing.

*Why:* disgo dispatches synchronously. A command body there held the socket
unread for as long as it took — seconds, for a hundred-item playlist — and an
interaction arriving in that window was acknowledged past Discord's three-second
deadline. It also made `COMMAND_PARALLELISM` describe a property the runtime
did not have.

*Enforcement:* **tested** — `discord.TestDispatchDoesNotRunTheCommandOnTheCallersGoroutine`,
`TestTwoGuildsRunAtTheSameTime`, `TestOneGuildStaysSequential`, and the
`cmdqueue` suite.

## What is not enforced

Recorded because an honest list of gaps is worth more than a clean one.

- **Rule 4's blind spot.** The check does not follow calls into helpers.
- **`Handler.Run(ctx interface{})`.** A command dispatched with the wrong
  context type returns nil and does nothing, silently. Ten commands open with
  that assertion. Nothing would notice a dispatch path that stopped working —
  which is how the mention path stayed dead long enough to be deleted.
- **Reply errors.** Twenty-seven ignored `Followup*`/`Respond*` calls in
  `internal/command`. Consistent, and consistently unchecked.
- **`kkdai`'s `init()`** rewrites a third-party package's global for the whole
  process on import. The library offers no narrower knob; an embedder gets no
  warning.
- **A UDP write that fails with anything but a closed socket.** disgo logs it
  and keeps pulling, so a whole track can drain into a socket delivering
  nothing while everything above reports normal playback. Melodix cannot
  observe it. The log bridge names it `voice_audio_send_failed` so it can at
  least be counted, and that is a mitigation, not a fix.

## Adding a rule

A rule that cannot name its enforcement does not belong here yet. Write the
check first — it is usually twenty lines of reading your own source — then
*verify the check fails* when you reintroduce the shape it forbids. Every check
above was confirmed that way, and the first version of rule 4's did not catch
the very defect it was written for.
