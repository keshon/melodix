# Ownership

Who may write what, and what stops the rest.

Go will not answer that for you. Rust refuses to compile a second writer and
Erlang puts it in another process; Go hands you a pointer and trusts you. So
the rules live here, and each one names what enforces it — because a rule
enforced only by review is a rule that gets broken by whoever is next in a
hurry.

Every rule below exists because its absence produced a real defect. The
forensic detail is in `docs/audit_2026-09-15_*`; this document is the rules
themselves.

## Enforcement grades

A rule's grade is the strongest thing that would stop a change violating it.

| Grade | Means |
|---|---|
| **compiler** | The violation does not typecheck. Needs no vigilance. |
| **refused** | It compiles and fails at runtime with an error naming the mistake. |
| **checked** | A test reads the source and fails on the shape. Survives refactors that behavioural tests do not. |
| **tested** | A test fails, usually under `-race`. Depends on the test driving the right interleaving. |
| **documented** | A comment says so. Review is the only enforcement — treat these as rules that will eventually be broken. |

`go test -race ./...` runs all of it.

---

## 1. A Track leaves the engine by value

`Player.CurrentTrack` returns `parsers.Track`. No exported player method hands
out a `*parsers.Track`.

**Because** a playing Track is written while it plays: the parser that actually
opened it, whether the stream is passthrough or cached, a title the media knew
and the resolver did not. Renderers read it from their own goroutines. A
pointer shares the writes; a value cannot.

**Enforced by** compiler — a caller holding a value has nothing to write back
into the engine — and checked:
`player.TestNoExportedMethodHandsOutATrackPointer`.

## 2. A stream owns its Track; findings travel back as values

`RecoveryStream` copies the Track it is given. Parsers write through the
pointer they are handed, and it lands on that copy. What the stream learned
returns as a `stream.OpenInfo`, from `Start` and from the confirmation
callback, which the player applies under its own lock.

**Because** `Streamer.Open(track *Track, …)` is an out-parameter, and five
parser packages write through it. If that pointer belongs to the player, the
parsers are writing the player's state.

**Enforced by** tested: `stream.TestRecoveryStreamDoesNotWriteTheCallersTrack`.

## 3. A stream's run state has one writer, and it is whoever reads it

Parser index, position, retry counts, cache writer, Track copy: all belong to
the goroutine pulling packets. `Start` opens once, before a reader exists.
After that the only way in is `RequestReopen`, which raises a flag and closes
the source to unblock a parked read; the reader performs the reopen itself.

There is no exported accessor for what a stream is playing. Copying the answer
out does not help — the read itself races the writer.

**Because** recovery rewrites every one of those fields, and one of them is a
map. A concurrent map write is a runtime crash, not an error you can handle.

**Enforced by** compiler — `open` is unexported, so no other package can call
it — refused — a second `Start` returns an error rather than performing it —
and tested: `stream.TestRequestReopenDoesNotRaceTheReadAheadProducer`,
`TestAStreamCannotBeStartedTwice`.

## 4. `p.mu` is never held across I/O

Capture what the call needs under the lock, release, then call.

**Because** the player's outward edges are a voice join (15s), a voice leave
(10s) and a stream open (a fetch, or an ffmpeg process). The lock guards one
guild's queue, so anything held across those makes every reader of that queue
wait for a network round trip.

**Enforced by** checked: `player.TestLockIsNeverHeldAcrossIO` scans `player.go`
for the outward interfaces' method names inside a locked region. It matches
method names rather than receivers, because moving a call out from under a lock
starts by capturing the receiver into a local — so a receiver-keyed check goes
quiet exactly when someone puts one back.

**Limit:** it does not follow calls. A helper called under the lock that itself
does I/O passes this and is still wrong.

## 5. A run resets only its own state

Every playback run carries a generation. `Stop`, `clearIfCurrent`, the
confirmation callback and the queue-end teardown all compare against it before
writing.

**Because** `Stop` waits up to ten seconds for the playback goroutine to exit.
A track can end and the next begin inside that wait, so the run that is current
when it finishes is not the one it was called for.

**Enforced by** tested: `player.TestStopDoesNotResetANewerRun`,
`TestASupersededRunDoesNotClearTheCurrentOne`,
`TestAQueueEndTeardownDoesNotStopTheTrackThatFollowedIt`.

## 6. Nothing reachable from a Player has session lifetime

A `voicesink.Provider` holds no voice manager and no DAVE registry. It resolves
both per acquisition through a `Resources` function.

**Because** a `Player` outlives every gateway session, and both of those die
with one: a voice manager belongs to a single `bot.Client` and closes over that
client's gateway, and a DAVE registry is built fresh per session. Held across a
reconnect, a join is addressed to a gateway that has been shut — which fails
instantly and permanently, and which invalidating the sink does not fix,
because what is stale is the thing that makes connections.

**Enforced by** checked —
`voicesink.TestAProviderStoresNothingThatDiesWithTheSession` reflects over the
struct — and tested: `TestAProviderUsesTheLiveSessionAfterARestart`.

The violation to watch for is "resolve once in the constructor and keep it". It
reads as a tidy-up.

## 7. Connection liveness is the voice manager's answer

The provider keeps the connection it opened only to recognise it again, and
compares it by identity. It never reads protocol state off it.

**Because** disgo removes a connection on a voice websocket close it cannot
resume from, and on closing the client, and announces neither. A field of ours
can only hold a memory of an answer. Acquiring without asking hands the next
track a sink over a dead socket; releasing without asking spends the close
budget waiting on a gateway that is not there. Reading protocol state off the
connection races the gateway goroutine, which writes it without a lock.

**Enforced by** checked —
`voicesink.TestEveryPathThatDecidesLivenessAsksTheManager` — and tested:
`TestAConnRemovedByTheLibraryForcesARejoin`,
`TestReleasingAConnTheLibraryAlreadyTookDoesNotCloseIt`.

## 8. A sink never emits a frame its transport cannot protect

While the guild's DAVE session reports no live epoch, the frame provider
withholds frames, without consuming the packets it holds — so the hold is a
pause rather than a gap. A hold that outlasts its budget ends the track as a
transport failure.

**Because** dave-go forwards a frame unmodified rather than failing when no
ratchet is selectable, and disgo's send path never asks. An unprotected frame
on an end-to-end encrypted channel is dropped by every receiver expecting
encryption, and reaches the server with no end-to-end layer.

**Enforced by** tested — `voicesink.TestHoldStopsSendingWithoutAnEpoch`,
`TestHoldThatNeverResolvesEndsTheTrack` — and documented in `pkg/music/sink`'s
`AudioSink` contract, whose silence is what let this property disappear during
a library swap.

## 9. A sink detects its own transport dying

`Sink.Stream` watches two things it can observe: the audio sender has stopped
asking for frames, and the connection it was built on is no longer the guild's.

**Because** disgo offers one signal for a connection dying under a running
track, `OpusFrameProvider.Close`, and v0.19.6 never calls it. A sink that waits
for it blocks forever: the track never ends, the queue never advances, and the
player's transport recovery sits behind an error nothing produces.

**Enforced by** tested —
`voicesink.TestClosedSocketEndsTheTrackAsTransportFailure`,
`TestAConnRemovedBehindOurBackEndsTheTrack`, both against disgo's real audio
sender — and documented in the `AudioSink` contract.

## 10. The gateway read loop dispatches and returns

Command bodies run on a per-guild worker. `Ready` and `GuildJoin` stay inline;
their ordering is load-bearing.

**Because** disgo dispatches events synchronously. A command body on that
goroutine holds the socket unread for its whole duration, and Discord's
acknowledgement deadline is three seconds from the interaction's creation, not
from its delivery.

**Enforced by** tested:
`discord.TestDispatchDoesNotRunTheCommandOnTheCallersGoroutine`,
`TestTwoGuildsRunAtTheSameTime`, `TestOneGuildStaysSequential`.

---

## Not enforced

Known gaps. An honest list is worth more than a clean one.

- **Rule 4 does not follow calls** into helpers invoked under the lock.
- **`Handler.Run(ctx interface{})`** returns nil on a context-type mismatch, so
  a command dispatched wrongly succeeds and does nothing. Ten commands open
  with that assertion, and nothing would report a dispatch path that stopped
  working.
- **Reply errors** are ignored at 27 call sites in `internal/command`.
  Consistent, and consistently unchecked.
- **`kkdai`'s `init()`** rewrites a third-party package's global for the whole
  process on import. The library offers no narrower knob.
- **A UDP write failing with anything but a closed socket** is logged by disgo
  and ignored, so a track can drain into a socket delivering nothing while
  everything above reports normal playback. Melodix cannot observe it. The log
  bridge names it `voice_audio_send_failed`, which is a mitigation, not a fix.

## Adding a rule

A rule that cannot name its enforcement does not belong here yet.

Write the check first — it is usually twenty lines of reading your own source
— then **verify it fails** when you reintroduce the shape it forbids. Every
check above was confirmed that way, and rule 4's first version did not catch
the defect it was written for.
