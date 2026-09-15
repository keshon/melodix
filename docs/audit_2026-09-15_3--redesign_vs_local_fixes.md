Architecture pass. Let me first ground the cost estimates in actual call-site counts rather than guessing.

Three measurements first, because they change the cost calculus decisively:

| Boundary | Production call sites |
|---|---|
| `sink.Provider` inside the engine | **4 lines**, all in `player.go` (377, 557, 560, 588) |
| `RecoveryStream` outside `pkg/music/stream` | **6 lines**, all in `player.go` |
| `Player` public methods, all consumers | 58 sites / 7 files |

And one structural fact that reframes everything below: `voice.Service` **already** holds the transport as a factory —

```go
type SinkProviderFactory func(guildID string) musicsink.Provider   // service.go:30
b.voice = voice.NewVoiceService(b.sessionAPI, b.newSinkProvider, …) // bootstrap
```

`Bot.newSinkProvider` resolves `b.currentConn()` freshly on every call. The capability-resolution pattern is already correct and complete at the Bot↔Service boundary. Then:

```go
provider, ok := s.sinkProviders[guildID]        // service.go:187-190
if !ok { provider = s.newSinkProvider(guildID); s.sinkProviders[guildID] = provider }
...
sinkProvider: sinkProvider,                     // player.go:201  (write-once)
```

**Two lines of memoization destroy an otherwise-correct design.** That is the single most important sentence in this pass.

---

# Part 1 — Is this the right ownership model?

Mostly yes, with one inversion.

The model is right about the big thing: `pkg/music` does not know Discord exists, and the boundary is mechanically enforced. That is worth preserving and is why the migration was survivable at all.

The model is wrong about one thing, and it is the same mistake twice at two different scales:

```
Player (process lifetime) ──holds──> SinkProvider (session lifetime)   ← M-03
SinkProvider              ──holds──> voice.Conn   (disgo-owned)        ← M-06
```

In both cases a longer-lived object **stores** a reference to a shorter-lived one instead of **resolving** it. That is not a Discord problem or a concurrency problem; it is a lifetime-direction problem, and it produces the same failure both times: the holder keeps using something that no longer exists, and nothing tells it.

The tree in your diagram is not wrong. The arrow from `Player` to `SinkProvider` is wrong — it should be a resolution, not a possession.

---

# Part 2 — Lifetimes

| Object | Owner | May replace | May invalidate | Parent dies → | Survives session restart | Survives voice restart | Safely cacheable |
|---|---|---|---|---|---|---|---|
| **Bot** | `main` | nobody | nobody | process exits | n/a | n/a | n/a |
| **Storage** | Bot | nobody | nobody | closed at exit | yes | yes | yes |
| **MusicEngine** (`voice.Service`) | Bot | nobody | nobody | — | **yes** | yes | yes |
| **Player[guild]** | MusicEngine | MusicEngine only | nobody | queue lost | **yes** | **yes** | yes, by guild |
| **Queue** | Player | Player | `Stop(true)` | — | yes | yes | n/a (owned) |
| **Track (queued)** | Queue | — | — | — | yes | yes | by value only |
| **Track (current)** | the run | the run | — | — | yes | yes | **by value only** |
| **Run / PlaybackRun** | Player | Player (new generation) | `Stop`, transport failure | goroutine exits | **no** — must be re-established | **no** | never |
| **RecoveryStream** | the run | nobody | run end | closed | no | no | never |
| **BufferedReader goroutine** | RecoveryStream | nobody | `Close` | exits | no | no | never |
| **Session** | `RunSession` | `RunSession` | watchdog | all below die | — | — | never |
| **bot.Client** | Session | Session | — | closed | no | — | never |
| **VoiceManager** | bot.Client | — | `Client.Close` | conns emptied | **no** | — | **never** |
| **DaveRegistry** | Session | Session | — | orphaned | **no** | — | **never** |
| **VoiceProvider[guild]** | Session | Session | — | dies with session | **no** | yes | **only within a session** |
| **voice.Conn** | VoiceManager (**authoritative**) | VoiceManager, disgo | gateway close, kick, `RemoveConn` | removed | no | **no** | **never — must be looked up** |
| **DAVE Session** | voice.Conn | ConnCreateFunc hook | `Forget`, conn death | dies with conn | no | no | never |
| **Sink** | the run | per acquisition | transport failure | — | no | no | never |
| **OpusFrameProvider** | Sink | per `Stream` call | transport failure | — | no | no | never |
| **Command execution** | dispatcher | — | ctx cancel | abandoned | no | — | n/a |

The row that matters: **`VoiceProvider[guild]` is session-scoped and is currently cached at process scope.** The row beneath it: **`voice.Conn` is disgo-owned and is currently mirrored.**

---

# Part 3 — The alternative model

Your proposed split is correct and it is what the lifetime table above implies:

> The music engine owns logical playback state. The Discord session owns transport resources. A voice connection is disposable. A playback run must not directly own session-lifetime resources.

It is superior to the current model, but not because it introduces anything. It is superior because it makes one rule checkable: **no field reachable from a `Player` may have session lifetime.** Today that rule is violated by exactly one field (`Player.sinkProvider`), and the violation is invisible because the field's *type* is an interface that says nothing about lifetime.

The last clause — "a playback run must not directly own session-lifetime resources" — is the whole architecture. Everything else in your diagram already holds.

---

# Part 4 — Three designs for the seam

### Design A — current model + generation invalidation

`RunSession` bumps a counter; `GetOrCreatePlayer` (or a session-change hook) compares and rebuilds the provider, then **writes it into the existing Player**.

- Correctness: needs a new setter on `Player`, writing a field that the playback goroutine reads — a *new* race where none existed. Needs a notification path from `RunSession` into `voice.Service`, which does not exist today.
- Lifetime clarity: worse. The Player still stores a session-lifetime object; you have merely added machinery to keep the lie fresh.
- Moving parts: +1 counter, +1 setter, +1 notification path, +1 lock.
- **Verdict: rejected.** It adds three mechanisms to preserve the defect.

### Design B — provider factory

`Player` stores `sinkFactory func(guildID string) sink.Provider` instead of `sinkProvider sink.Provider`. The four call sites become `p.sinkFactory(p.guildID).Sink(target)` etc. `voice.Service` stops memoizing and passes `s.newSinkProvider` straight through. The session owns a per-guild provider cache internally (so connection reuse across tracks is preserved).

- Correctness: **eliminates M-03 by construction.** No session-lifetime value is reachable from a `Player` field. Between sessions the factory yields `deadSinkProvider{}`, which is already the designed answer.
- Complexity: **negative.** Deletes `Service.sinkProviders`, deletes its nil-guard, deletes a map.
- Lifetime clarity: the rule becomes type-checkable by inspection — grep `Player` for stored interfaces.
- Reconnect: correct, **but only in combination with a transport-failure signal.** Mid-track, `runPlayback` is blocked in `Stream`; without M-02 fixed it never re-acquires and the factory never gets consulted. AC-1 and AC-2 are coupled.
- Testability: better — the factory is one function to fake, and generation behaviour becomes assertable without a Discord session.
- Cost: ~10 lines in `player.go`, ~8 removed in `service.go`, 2 constructor sites in CLI.
- Future races: fewer — one write-once field disappears rather than gaining a writer.

### Design C — explicit transport object

Introduce `Playback → VoiceTransport → {voice.Conn, DAVE}` as a new named layer.

- What it adds over B: nothing at the lifetime level. B already severs the bad edge.
- What it costs: a third name for a concept that already has two (`sink.Provider`, `AudioSink`) in a repo where `Provider` already means two different things in two packages both called `sink`.
- **Verdict: rejected as a new type.** Its *semantics* are needed (Part 5); its *packaging* is not.

**Pick: Design B.** It eliminates M-03, reduces total code, and is the smallest diff of the three.

Does B eliminate M-06? **No.** M-06 lives one level down, inside the Provider's own `p.conn`/`p.currentChannelID` mirroring. B changes who holds the Provider; it does not change what the Provider believes. M-06 needs its own change (AC-3 below), which is also a net deletion.

---

# Part 5 — Should `VoiceTransport` be introduced?

**No. You already have it, and adding a second one would be architecture theatre.**

Compare your proposed contract to what exists:

```
proposed                     existing
Connect(channel)        ≈    Provider.Sink(target) (AudioSink, error)
Send/ProvideAudio(...)  ≈    AudioSink.Stream(r opus.Reader, stop <-chan struct{}) error
WaitForFailure(...)     ≈    ...the error return of Stream — it blocks and reports
Close(...)              ≈    Provider.ReleaseSink(target) / InvalidateSink()
```

`AudioSink.Stream` *is* "send, and block until failure or end". The abstraction is not missing. What is missing is two clauses in its contract:

```go
// AudioSink consumes a stream of 20ms Opus packets. The sink owns the read
// loop; Stream returns when the stream ends (io.EOF) or stop is closed.
```

That doc comment is the root cause of M-01 and M-02 in one sentence. It defines the happy path and is silent on:

1. **What a sink must do when its transport dies.** The fork's implementation produced `ErrVoiceTransport`; the contract never required it; deleting the implementation deleted the requirement, and the player's entire recovery subsystem became unreachable with nothing failing.
2. **What a sink must not emit.** The fork's `holdFrames` gate was an implementation detail; the contract never said "a sink must not emit frames the transport cannot protect"; deleting the implementation deleted the safety property, and E2EE silently degraded.

> Would a `VoiceTransport` abstraction let us hide disgo's broken lifecycle semantics, so M-02 becomes a transport implementation detail rather than a Player/Sink concern?

It would — and so does the existing one, for free, the moment the contract says so. M-02 is *already* supposed to be a Sink implementation detail: the Sink is supposed to return `ErrVoiceTransport` and `runPlayback` already knows what to do with it. The architecture is right; the Discord implementation cannot honour it because disgo never calls `OpusFrameProvider.Close()`.

So the correct change is **not** a new layer. It is:

- **AC-2**: write the two missing clauses into the `AudioSink`/`Provider` doc contract, and make the Discord sink detect transport death itself rather than waiting for a callback disgo does not make. The detection point already exists — `Sink.Stream`'s `select` — it needs a third case.
- **AC-3**: make the Provider treat `manager.GetConn(guildID)` as authoritative. This *deletes* `p.conn` and most of `p.mu`'s job.

Being critical about my own recommendation: AC-3 has a real caveat. `connImpl.ChannelID()` reads `c.state.ChannelID`, which `HandleVoiceStateUpdate` writes from the gateway goroutine with no lock — an upstream race. So AC-3 should compare **pointer identity** (`manager.GetConn(gid) == theConnThisSinkWasBuiltOn`) rather than reading disgo's channel field, and keep melodix's own `currentChannelID` alongside the conn identity, since that value melodix owns and never mutates for a given conn. That gives authority without inheriting disgo's race.

---

# Part 6 — Player concurrency model

Honest evaluation against the verified findings:

| Model | M-04 | M-07 | M-08 | M-09 | Cost |
|---|---|---|---|---|---|
| **A — current mutex** | latent | present | present | present | 0 |
| **B — single-owner event loop** | eliminates | **unaffected** | **unaffected** | eliminates | very high |
| **C — mutex + generation + I/O boundary rule** | eliminates | unaffected | unaffected | eliminates | ~15 lines |

The critical observation: **an actor Player does not touch M-07 or M-08.**

- M-08 lives in `RecoveryStream`, driven by the `BufferedReader` goroutine, which is *outside* the Player entirely. No amount of Player serialization reaches it.
- M-07 is a pointer escaping through `CurrentTrack()`. An actor Player that returns `*parsers.Track` from its loop has exactly the same bug. Only returning a value fixes it, and you can do that today without an actor.

So B buys M-04 (which the forensic pass could not reproduce in 47 rounds) and M-09 (a one-line lock-scope bug), at the cost of rewriting the single trickiest 730-line file in the repository — the one with a concurrent hammer test, careful per-run channel ownership, and `clearIfCurrent`'s identity guard already correct. The migration's lesson is that rewriting working concurrency loses invariants nobody wrote down. Doing it again voluntarily, to fix one unproven finding and one one-liner, is the wrong trade.

**Pick: Design C.** Concretely, three things, not a rewrite:

1. **`Stop` takes `playNextMu`** for its duration, or captures `(currTrack, playbackDone)` in phase 1 and resets in phase 2 only if both are unchanged — the same identity guard `clearIfCurrent` already uses. Eliminates M-04's shape.
2. **A stated rule: no blocking I/O under `p.mu`.** One violation today (`ReleaseSink` at player.go:377). Eliminates M-09.
3. **An explicit run generation** (an `int64` incremented in `startTrack`, carried into `runPlayback`) replacing pointer-identity comparison. This is cheap and it makes the existing invariant legible; today "is this my run?" is encoded as `p.currTrack == track`, which works but is not obviously a generation check and is why `Stop` was written without one.

---

# Part 7 — Track ownership

| Model | Fit | Verdict |
|---|---|---|
| **A — snapshot at API boundaries** | `cloneTrack` already exists and is already used at two of three exits | **pick** |
| **B — immutable Track** | `RecoveryStream.Open` legitimately mutates `CurrentParser`/`Passthrough`/`Cached` mid-track; going immutable means threading a new value back up through `packetView` → `BufferedReader` → `Sink` → `Player`, i.e. redesigning the packet path to carry metadata | rejected |
| **C — Track with internal synchronization** | puts a mutex on a value type that is copied into queues, sent to storage, and rendered — `slices.Clone(p.queue)` would copy a mutex | **rejected outright** |

The desired property — *UI/status code should never share mutable engine state* — is correct and is already the codebase's own convention: `cloneTrack` guards the recorder (`rec.Record(gid, time.Now(), cloneTrack(*track))`) and the failure snapshot (`failedSnapshot := cloneTrack(*track)`). `CurrentTrack()` is the one exit that was missed.

Fix: `CurrentTrack()` returns `parsers.Track` by value (or `*parsers.Track` pointing at a clone). Note the doc comment currently says *"The pointer identity is meaningful: a playback run owns its own `*parsers.Track`, compares against this to tell itself apart from a newer run"* — that identity use is **internal** (`clearIfCurrent`, `onParserConfirmed`), and if you adopt the explicit generation from Part 6 it stops depending on the exported accessor at all. The two changes reinforce each other.

Cost: ~5 lines, plus 4 external call sites that currently nil-check the pointer.

---

# Part 8 — RecoveryStream ownership

This is the most interesting design question in the set, and "put a mutex around everything" is indeed the wrong answer — it would mean taking a lock on the 50 Hz hot path to protect state that has no business being shared.

The real question you posed is the right one:

> Should these fields even be mutable from multiple goroutines?

**No.** All eight fields (`retries`, `parserIndex`, `seekSec`, `curParser`, `firstRead`, `fromCache`, `cacheDisabled`, `cacheWriter`) are per-stream *run* state. They have exactly one logical owner: whoever is pulling packets. The design comment already declares this invariant —

> *"Every other field belongs to the producer goroutine alone, and Close reaches them only after Wait proves it has exited."*

— and `Close` honours it (`Stop → closeCurrent → Wait`). **Exactly one method violates it: `ReopenAfterTransportFailure`**, which calls `closeCurrent()` + `Open()` from the player goroutine with no `Stop`/`Wait`.

| Design | Assessment |
|---|---|
| **A — internally synchronized** | A lock on `ReadPacket`'s hot path to protect state only one goroutine should touch. Fixes the symptom, keeps the confusion, adds contention at 50 Hz × guilds. |
| **B — enforce single-goroutine ownership** | Restores the invariant the code already claims. **Pick.** |
| **C — split immutable config from mutable run state** | Correct in principle (`track`, parser list, logger, cache key are immutable; the eight fields are not), but as a *separate* change it is a type reshuffle that does not by itself stop two goroutines calling `Open`. It is the documentation of B, not a substitute. |

**B, concretely, using machinery that already exists.** The reopen becomes a *request* rather than a call:

- `ReopenAfterTransportFailure` sets a flag and calls `closeCurrent()`. `closeCurrent` already takes `rs.mu` and already tears the source down specifically to unblock a producer parked in a read — that is its documented job.
- The producer, on its next read error, sees the flag and performs the `Open` itself, on the goroutine that owns the state.
- The player learns the outcome through the packet stream it is already reading, as it does for every other recovery.

This is one flag and one branch. It removes all 27 races without a single new lock on the hot path, and it makes the stated invariant true instead of aspirational. It also matches how every *other* recovery in `RecoveryStream` already works: parser switch and early-EOF reopen both happen inside `ReadPacket`, on the producer. Transport reopen is the odd one out, and that is precisely why it is the one that races.

Edge case to design for: if the producer is blocked in a read on a socket that never returns, `closeCurrent()` is what unblocks it — already true today. If the buffer is disabled (`BUFFER_AHEAD_MS=0`), `ReadPacket` runs on the sink's goroutine, which is the player's own run goroutine, and the race does not exist at all. Both configurations converge on "one goroutine drives the stream," which is the point.

---

# Part 9 — Event dispatch

| Design | ACK latency | Ordering | Per-guild order | Cross-guild parallel | Backpressure | Shutdown | Complexity |
|---|---|---|---|---|---|---|---|
| **A — global async** | good | **lost** | **lost** | yes | **none** | hard | low code, high risk |
| **B — spawn command bodies** | good | kept | **lost** | yes | `execguard` (finally live) | easy | ~10 lines |
| **C — internal queue + workers** | good | kept | configurable | yes | explicit | medium | ~100 lines |
| **D — B + per-guild serialization** | good | kept | **kept** | yes | `execguard` + per-guild lock | easy | ~30 lines |

**Global async (A) is specifically wrong for this repository.** `eventManagerImpl.DispatchEvent` spawns a goroutine **per listener per event** when async is on. Melodix registers six listeners, five of which type-assert and return immediately. Combined with `gateway.IntentsAll` — which includes `GuildPresences` and `GuildMembers` — a busy guild would spawn six goroutines per presence update, unbounded, with no backpressure, to run five no-ops. That is a worse failure mode than the one being fixed.

**Your hypothesis is correct: gateway serialized + commands asynchronous + per-guild Player serialized is the better model.** That is Design D, and it is right for three reasons specific to this codebase:

1. **Gateway ordering is load-bearing here.** `Ready` → `syncer.SyncGuildCommands` and `GuildJoin` → same must not interleave with each other or with themselves; the `perGuildLocks sync.Map` in `cmdsync` exists precisely because someone anticipated this. Keeping the gateway loop serial keeps that guarantee free.
2. **Per-guild serialization matches the domain.** A guild's music is inherently sequential — you cannot meaningfully `/play` and `/next` at the same instant. Serializing per guild costs nothing anyone wants.
3. **It keeps M-04 from widening.** Be precise about what this does and does not buy: M-04's two actors are a command goroutine and the completion goroutine. Per-guild serialization serializes commands against commands only. It therefore **prevents the M-05 fix from widening M-04; it does not eliminate M-04.** Relying on it instead of fixing `Stop` would be exactly the undocumented-timing-assumption pattern this audit series has been criticizing. Do both.

Also fix in the same pass, since they are the reason the loop is slow in the first place: `cmdsync`'s `time.Sleep(25ms)` per write (disgo's REST client already rate-limits), and — worth considering — narrowing `IntentsAll`/`FlagsAll`, which is what makes the event volume large enough for any of this to matter.

One consequence to accept deliberately: with D, `COMMAND_TIMEOUT` still enforces nothing, because `Adapter.Run` drops the context. Making dispatch async without threading the context means a slow command now occupies a worker instead of the gateway — better, but still uncancellable. Either thread the context or delete the knob; do not ship D while still advertising a timeout that does not exist.

---

# Part 10 — One change, many findings

Strict marking. "ELIMINATES" means the failure class cannot occur, not that it becomes easier to fix.

| Architectural change | M-01 | M-02 | M-03 | M-05 | M-06 | M-07 | M-08 | M-09 |
|---|---|---|---|---|---|---|---|---|
| **AC-1** Player holds sink *factory*, not provider | — | — | **ELIM** | — | — | — | — | — |
| **AC-2** Complete the sink contract: transport-death signal + no-emit-when-unsafe; Discord sink detects death itself | **ELIM** | **ELIM** | — | — | RED | — | ⚠ activates | — |
| **AC-3** `VoiceManager` is authoritative; delete `Provider.conn` mirror | — | RED | — | — | **ELIM** | — | — | RED |
| **AC-4** RecoveryStream single-goroutine ownership (reopen as request) | — | — | — | — | — | RED | **ELIM** | — |
| **AC-5** Track leaves the engine by value | — | — | — | — | — | **ELIM** | — | — |
| **AC-6** Command bodies off gateway goroutine, per-guild serialized | — | — | — | **ELIM** | — | — | — | RED |
| **AC-7** No blocking I/O under `p.mu` (+ `Stop` identity guard) | — | — | — | — | — | — | — | **ELIM** |
| **AC-8** Actor/event-loop Player *(rejected)* | — | — | — | — | — | — | — | ELIM |
| **AC-9** New `VoiceTransport` type *(rejected)* | — | — | — | — | — | — | — | — |

Reading the matrix honestly:

- **No single change eliminates a large class.** The findings are genuinely independent defects at four different boundaries; there is no one architectural lever.
- **AC-2 is the highest-leverage single change** (two eliminations, one reduction) — and it is the one that is *not* a structural change at all, but a contract change plus an implementation. Which is the diagnosis: the architecture was fine and the contract was empty.
- **AC-8 (the big rewrite) has the worst ratio in the table.** One elimination, for the largest diff. The matrix is the argument against it.
- **AC-2 activates AC-4's finding.** This is the only hard ordering constraint in the whole plan.

---

# Part 11 — Architecture-dependent vs independent

## A. Architecture-dependent (do not fix before deciding)

**M-02 — transport failure detection**
- *Why architecture matters:* the fix is "who is responsible for noticing, and how do they report it." That is a contract question, and contracts are architecture.
- *Which decision affects it:* whether `sink.Provider`/`AudioSink` gains the failure clause (AC-2) or a new `VoiceTransport` layer is introduced (AC-9).
- *Wrong premature fix:* bolting a polling ticker into `Sink.Stream` with no contract change. It works, and the next backend swap loses it again for exactly the reason this one did.

**M-06 — mirrored connection state**
- *Why architecture matters:* the fix is deciding who is authoritative. If melodix keeps mirroring, you need reconciliation machinery; if disgo is authoritative, you delete the mirror.
- *Which decision affects it:* AC-3. It also changes what `Provider.mu` is for, which interacts with M-09.
- *Wrong premature fix:* adding a `RemoveConn` callback hook to keep the mirror fresh. That preserves two sources of truth and adds a third mechanism.

**M-03 — provider lifetime**
- *Why architecture matters:* it is purely a lifetime-ownership decision (Design A vs B).
- *Which decision affects it:* AC-1.
- *Wrong premature fix:* Design A — a generation counter plus a setter on `Player`. It creates a new write to a field the playback goroutine reads, i.e. a new race, to preserve a design that should be deleted.

**M-05 — dispatch model**
- *Why architecture matters:* A/B/C/D differ in ordering guarantees, not in effort.
- *Which decision affects it:* AC-6, and specifically whether per-guild serialization is part of it.
- *Wrong premature fix:* enabling `WithAsyncEventsEnabled()`. One line, looks like the obvious answer, and with `IntentsAll` it spawns six unbounded goroutines per presence event.

## B. Architecture-independent (safe now, under any design)

**M-01 — DAVE frame gating.** The gate belongs in `frameProvider.ProvideOpusFrame` under every design considered. `DaveRegistry` already holds the session; `NewProvider` already receives it. Correct under A, B, C, and D. **Ship it first.**

**M-07 — Track snapshot.** `CurrentTrack()` returning a value is correct under every model, including the actor Player. Independent of AC-4.

**M-09 — `ReleaseSink` outside `p.mu`.** Capture, unlock, call. Correct under every design; AC-8 would make it unnecessary, but AC-8 is rejected.

**M-04 hardening — `Stop`'s identity guard.** Unproven as a live bug, but the guard is correct under every model and is a prerequisite for AC-6.

**M-08 — RecoveryStream ownership.** Architecture-independent *in substance* (nothing outside `player.go` touches `RecoveryStream`), but **ordering-dependent**: it must land before AC-2, because AC-2 is what makes the racing path reachable.

### Fixes that become unnecessary under the chosen architecture

- **A generation/invalidation mechanism for providers** — unnecessary under AC-1. Do not build it.
- **Reconciliation machinery for `Provider.conn`** — unnecessary under AC-3, which deletes the field being reconciled.
- **A mutex over `RecoveryStream`'s eight fields** — unnecessary under AC-4, which removes the second writer instead of protecting against it.
- **`Player.SetSinkProvider`** — unnecessary under AC-1; actively harmful.
- **Making `RecoveryStream` an interface for testability** — unnecessary; `stream.SetRegistry` already provides the seam and the existing tests use it.

---

# Part 12 — The minimum architectural change

**Two sentences, one field, one contract.**

> **1. No field reachable from a `Player` may have session lifetime.** `Player` holds `sinkFactory func(guildID) sink.Provider` instead of `sinkProvider sink.Provider`; the session owns the per-guild provider cache.
>
> **2. The audio-sink contract must state how a transport fails and what a sink must never emit.** `AudioSink`/`Provider` gain those two clauses, and the Discord implementation honours them: it detects transport death itself, and it holds frames while DAVE is not ready.

That is the minimum. Everything else in this document is either a consequence of those two (AC-3 falls out of "who is authoritative" once the contract names failure), a local bug fix (M-01's gate, M-07's clone, M-09's lock scope), or an execution-model choice that is independent of both (AC-6).

Measured cost of the architectural part: **~10 lines added in `player.go`, ~8 deleted in `service.go`, `Provider.conn`/`currentChannelID` deleted, two doc comments rewritten, two constructor call sites in the CLI updated.** The architecture change is a net reduction in code, because the correct pattern already exists one layer up and is being defeated by a cache.

---

# Part 13 — What not to change

- **Do not introduce `VoiceTransport`.** `sink.Provider` + `AudioSink` already are it. A third name for the concept, in a repo that already has two packages named `sink` and two types named `Provider`, is a net loss in comprehensibility.
- **Do not convert `Player` to an actor.** One elimination for the largest diff in the repo, and it touches neither M-07 nor M-08.
- **Do not enable disgo's global async events.** Unbounded goroutines per listener per event, under `IntentsAll`.
- **Do not replace mutexes with channels.** `p.mu` and `playNextMu` are correct; `rs.mu` is correct for what it covers. The bugs are scope and ownership, not primitive choice.
- **Do not add an event bus, a DI container, or a state-machine framework.** Nothing in the findings is caused by their absence.
- **Do not split `voice.Service`.** It does four jobs in 405 lines, which is arguably one too many — but splitting it fixes zero findings and destroys the reviewability of the diff that does.
- **Do not abstract disgo's REST.** `reply.API` already does it and `TestDiscordStaysBehindTheAdapter` already enforces it.
- **Do not rename the two `sink` packages in this work.** It is a real (P3) problem. Mixing a repo-wide rename into a concurrency fix makes both unreviewable. Separate commit, separate week.
- **Do not rewrite the music engine.** It survived a library migration intact. That is evidence, not luck.
- **Do not fix `COMMAND_TIMEOUT` by adding contexts everywhere.** Either thread it through the four call sites that would honour it, or delete the knob. Do not ship a third option.

---

---

# Part 14 — Final recommendation

## 1. Fundamental diagnosis

**RC-1 — The engine↔transport contract specifies only the happy path.** `AudioSink`'s doc says it returns "when the stream ends (io.EOF) or stop is closed" and is silent on transport death and on what must not be emitted. Both properties existed in the discordgo fork's implementation and in nothing else, so deleting the implementation deleted the requirements — with no compile error, no failing test, and a whole recovery subsystem quietly becoming unreachable. **M-01, M-02.**

**RC-2 — Lifetime is expressed by holding a value where it should be expressed by resolving a capability.** The same error at two scales: `Player` (process) holds a `Provider` (session); `Provider` (session) holds a `voice.Conn` (disgo-owned, disposable). The pattern is invisible because the field types are interfaces that say nothing about lifetime. The correct pattern already exists one layer up — `Bot.newSinkProvider` resolves freshly every call — and two lines of memoization defeat it. **M-03, M-06.**

**RC-3 — `RecoveryStream` declares a single-owner invariant that one of its own methods violates.** The field comment states that only `reader`/`cleanup` are shared and that everything else belongs to the producer goroutine. `Close` honours it. `ReopenAfterTransportFailure` does not, and is the sole source of all 27 races. **M-08.**

**RC-4 — Mutable domain objects escape the engine by pointer.** `cloneTrack` exists and guards two of three exits, which proves the hazard was understood; `CurrentTrack()` is the one that was missed. **M-07.**

**RC-5 — The execution model was inherited, not chosen.** Nobody decided the bot should be single-threaded; disgo's default decided it, and the code still advertises `COMMAND_PARALLELISM=16`. ACK latency, head-of-line blocking and M-09's amplification are all emergent from a decision never made. **M-05, and M-09's blast radius.**

## 2. Recommended target architecture

```
PROCESS LIFETIME ─────────────────────────────────────────────────────┐
                                                                      │
  Bot                                                                 │
   ├── storage                                                        │
   └── MusicEngine  (today: voice.Service)                            │
        └── Player[guild]        ← survives session AND voice restart │
             │  state: queue, currTrack, generation  [guarded by p.mu]│
             │  RULE: no field here may have session lifetime         │
             ├── Queue                                                │
             └── Run (generation N)          ← dies with the track    │
                  └── RecoveryStream         ← SINGLE-GOROUTINE OWNED │
                       └── BufferedReader producer                    │
                            (the only goroutine that mutates          │
                             retries/parserIndex/seekSec/curParser/   │
                             firstRead/fromCache/cacheDisabled/       │
                             cacheWriter — reopen arrives as a        │
                             request, never as a foreign call)        │
                                                                      │
══════════ THE SEAM ══════════════════════════════════════════════════│
   Player holds:  sinkFactory func(guildID) sink.Provider             │
                  (resolved per acquisition — NEVER cached)           │
   Contract adds: · a sink MUST report transport death as             │
                    ErrVoiceTransport, detected by itself             │
                  · a sink MUST NOT emit a frame the transport        │
                    cannot protect (DAVE hold)                        │
══════════════════════════════════════════════════════════════════════│
                                                                      │
SESSION LIFETIME (one per RunSession; everything below dies together) │
                                                                      │
  Session                                                             │
   ├── bot.Client                                                     │
   │    └── VoiceManager        ← AUTHORITATIVE for conn liveness     │
   ├── DaveRegistry                                                   │
   └── VoiceProvider[guild]     ← session-owned cache, per guild      │
        │  holds conn IDENTITY + target channel; asks the manager     │
        │  whether that conn is still live. No mirrored state.        │
        └── voice.Conn          ← disgo-owned, disposable             │
             └── DAVE Session   ← per conn; gate consulted PER FRAME  │
                                                                      │
GATEWAY BOUNDARY ─────────────────────────────────────────────────────┘
  disgo listen goroutine  (stays serial — ordering is load-bearing)
        │  dispatch only: mark WS, mark ready, route
        ▼
  per-guild command worker  (execguard caps global parallelism;
                             per-guild lock preserves music ordering)
        │
        ▼  command body: REST, resolve, stream open — all off the loop
```

Concurrency boundaries, stated as checkable rules:

1. Nothing reachable from a `Player` field has session lifetime.
2. `RecoveryStream` state is mutated by one goroutine; reopen is a request.
3. `p.mu` is never held across I/O.
4. Domain objects leave the engine by value.
5. The gateway read loop dispatches and returns; it never executes a command body.
6. Connection liveness is answered by `VoiceManager`, never by a melodix field.

## 3. Architecture changes worth doing

Ranked by risk eliminated × findings affected ÷ (cost × migration risk):

| # | Change | Findings | Risk eliminated | Cost | Migration risk |
|---|---|---|---|---|---|
| 1 | **AC-2** — complete the sink contract; Discord sink detects its own transport death and gates on DAVE readiness | M-01, M-02 (elim), M-06 (red) | **highest** — one security property, one permanent-wedge class | medium | medium (activates M-08 — sequence it) |
| 2 | **AC-4** — RecoveryStream single-goroutine ownership; reopen as request | M-08 (elim), M-07 (red) | high — a `fatal error` process kill | low (~1 flag, 1 branch) | low, and it is a prerequisite for #1 |
| 3 | **AC-1** — Player holds the sink factory | M-03 (elim) | high — permanent per-guild playback loss after any reconnect | **negative** (net code deleted) | low |
| 4 | **AC-3** — VoiceManager authoritative; delete the conn mirror | M-06 (elim), M-02/M-09 (red) | medium-high | **negative** (deletes 2 fields) | low |
| 5 | **AC-7** — `Stop` identity guard + no I/O under `p.mu` | M-09 (elim), M-04 (elim) | medium — 15 s bot-wide stalls | very low | low |
| 6 | **AC-5** — Track leaves the engine by value | M-07 (elim) | medium | very low | low (4 call sites) |
| 7 | **AC-6** — command bodies off the gateway loop, per-guild serialized | M-05 (elim), M-09 (red) | medium — ACK failures under load | low-medium | medium (ordering semantics change; needs #5 first) |

Note that #3 and #4 both *reduce* total code. The architecture fix here is a deletion, not an addition.

## 4. Findings that can be fixed immediately

**M-01, M-07, M-09** — and the M-04 hardening.

All three are correct under every architecture considered in this document, including the ones I rejected. None of them is invalidated by any decision in §2.

**M-08** is substantively independent too (nothing outside `player.go` touches `RecoveryStream`), but it carries a hard ordering constraint and is listed in §5 for that reason alone.

## 5. Findings that should wait for architecture

**M-02** — wait for the contract decision (AC-2 vs AC-9). Fixing it without the contract clause reproduces the exact failure that lost it: a correct implementation with nothing recording the requirement.

**M-03** — wait for the Design A/B/C decision. The wrong fix (generation + setter) introduces a new race.

**M-06** — wait for the authority decision (AC-3). The wrong fix (a removal callback) preserves two sources of truth.

**M-05** — wait for the dispatch decision (A/B/C/D). The one-line fix (`WithAsyncEventsEnabled`) is worse than the bug under `IntentsAll`.

**M-08** — technically independent, but **must land before M-02**, because M-02's fix is what makes the racing path reachable. Treat it as architecture-sequenced.

## 6. Proposed implementation phases

**Phase 0 — Baseline harnesses.** Three test harnesses, no production change: (a) a pull-model transport harness — real `voice.NewAudioSender` + stub `Conn`/`UDPConn` — asserting that a dead transport surfaces as `ErrVoiceTransport`; (b) a `RecoveryStream` harness driving `ReopenAfterTransportFailure` against a live read-ahead producer under `-race`; (c) a fake `godave.Session` whose readiness can be flipped. All three currently fail, which is the point: they are the specification of Phases 1–3. *(All three exist as scratchpad probes from the forensic pass and can be moved in as-is.)*

**Phase 1 — Architecture-independent safety.** M-01 (per-frame DAVE gate + the four ported `dave_hold_test.go` cases), M-07 (`CurrentTrack` by value), M-09 (`ReleaseSink` outside `p.mu`), M-04 (`Stop` identity guard + explicit run generation). Harness (c) goes green. No structural change; shippable on its own.

**Phase 2 — RecoveryStream ownership.** AC-4: reopen becomes a request serviced by the producer. Harness (b) goes green. **Must precede Phase 3.**

**Phase 3 — The seam.** One coherent change to the transport boundary, in this order within the phase: AC-1 (factory) → AC-3 (authoritative manager, delete the mirror) → AC-2 (contract clauses + self-detected transport death). Harness (a) goes green. This is where "queues and players survive reconnects" becomes true for the first time — it requires AC-1 *and* AC-2 together, which is why they belong in one phase.

**Phase 4 — Execution model.** AC-6: command bodies off the gateway goroutine, per-guild serialization, `execguard` finally load-bearing. Same PR: delete `cmdsync`'s manual sleep, narrow `IntentsAll`/`FlagsAll`, and resolve `COMMAND_TIMEOUT` (thread it or delete it — not a third option). Depends on Phase 1's `Stop` guard.

**Phase 5 — Cleanup.** The P2/P3 backlog deferred from the first audit, each in its own commit: `atomic.Pointer` for the three holder structs, `rest.JSONErrorCodeInteractionAlreadyAcknowledged` instead of substring matching, delete the dead `onMessageCreate` path, delete `-check-disgo`, correct `docs/architecture.md` (it still claims a deleted test exists) and `docs/dave-todo.md` (it says the recovery timeout is unset; the code sets 5 s). The `sink` package rename, if wanted, is its own week.

**Phase 6 — Re-audit.** Re-run the full forensic pass against the new tree, plus the one thing static analysis cannot reach: a live run with a second account joining and leaving an E2EE channel mid-track, watching `dave.State().Stats.PassthroughFrames` and `TransitionWindows`. That number is the only direct measurement of whether M-01 is actually closed.

## 7. Answering your question directly

> If we are willing to change the architecture now, what should we change so that we don't spend the next month patching symptoms one by one?

Change **two things**, and neither is a redesign:

1. **Make the `Player`'s transport a capability it resolves, not a value it holds** — the factory already exists one layer up; delete the cache that defeats it.
2. **Write the failure and safety clauses into the audio-sink contract, and make the Discord sink honour them** — the abstraction is already correct and already the right shape; it is the contract that is empty, and that emptiness is precisely why a library swap silently removed end-to-end encryption and an entire recovery subsystem without breaking a build or a test.

Everything else on the list is a bug with a known fix. The month of symptom-patching you are trying to avoid comes from RC-1: properties that live only in an implementation get deleted with that implementation. Two doc comments and a harness that fails when they are violated is what stops the next migration from doing it again.