Re-inspecting before deciding. Starting with areas the previous passes did not trace: goroutine inventory, shared parser instances, cache/storage concurrency, and the CLI's use of `Player`.

I re-read the implementation. Three of my earlier conclusions were wrong or incomplete, and one new structural defect turned up. Those corrections are folded in below.

## What re-inspection changed

**1. The previous pass's "Player holds a factory" (Design B) is not the smallest correct mechanism — it is the wrong layer.** A factory in `Player` expresses a *Discord session* concern inside the Discord-free engine. The smaller fix keeps `Player.sinkProvider` write-once and makes the **provider itself** process-lifetime, resolving session resources internally. The engine is then untouched, and Invariant 5 is satisfied rather than violated. I am retracting Design B.

**2. M-03 and M-06 are the same bug.** Both are "cached pointer to a shorter-lived resource" inside `sink.Provider` (`p.manager`/`p.dave` from a dead session; `p.conn` from a dead manager). Delete both caches and both findings cease to be *representable*, not merely fixed.

**3. M-07 is bigger than reported, and its root cause is in the `Streamer` interface, not in `Player`.** New finding:

```go
// pkg/music/parsers/interface.go
Open(track *Track, seekSec float64) (opus.Reader, func(), error)   // ← out-parameter
```

Five parser packages write through that pointer — `kkdai/link.go:61-62`, `kkdai/pipe.go:31-32,47`, `scnative/link.go:20-21`, `ytdlp/link.go:62`, `ytdlp/pipe.go:44`, `ytnative/link.go:26,34,44` — setting `Title`, `Duration`, `Passthrough`. `RecoveryStream.Open` adds `CurrentParser` and `Cached`. That pointer *is* `Player.currTrack`, which `CurrentTrack()` publishes to the status watcher and the CLI. So **five mutable fields, three writers, two goroutines, one pointer.** The race detector flagged three fields; `Title` and `Duration` are equally exposed. I'll call this **S-1**; M-07 is its symptom.

**4. `pkg/music/cache/store.go` is a correct counterexample** — `register()` and `OpenAt()` explicitly do index I/O *outside* `s.mu`, with comments saying why. The codebase already knows Invariant 6. M-09 is a local slip, not a systemic pattern — which lowers its architectural weight.

**5. Two accepted lock-across-I/O sites exist and should stay:** `ytnative.visitorID` (HTTP under `visitor.mu`, ~once per 6 h, 10 s bound) and `soundcloudapi.Client.ClientID` (scrape under `c.mu`). Both are *deliberate* — the lock exists to collapse a stampede into one fetch. Documented as such. Not findings.

**6. All five `Streamer` structs are stateless** (`struct{}` / `struct{Mode}`), so sharing registry singletons across guilds is safe. No finding there.

---

# 1. Fundamental diagnosis

**RC-1 — Mutable domain state is passed downward as an out-parameter.** `Streamer.Open(track *Track, …)` lets five parser packages and `RecoveryStream` write into the object the Player publishes to the UI. There is already a correct upward channel for the same information (`SetOnParserConfirmed`), so the mutation is a *second, racy* path for facts that already have a first. → S-1, M-07.

**RC-2 — Transport objects cache references to resources they do not own.** `sink.Provider` caches `manager`/`dave` (session-lifetime) and `conn` (disgo-owned). Both go stale silently because nothing tells a cache its referent died. The correct pattern — resolve on use — already exists one layer up in `Bot.newSinkProvider` and is defeated by caching its result. → M-03, M-06.

**RC-3 — The audio-sink contract defines only success.** `AudioSink.Stream`'s doc says it returns "when the stream ends (io.EOF) or stop is closed". It is silent on transport death and on what a sink must refuse to emit. Both properties lived in the deleted fork implementation and in no contract, so the backend swap removed E2EE enforcement and the entire recovery subsystem without breaking a build. → M-01, M-02.

**RC-4 — `RecoveryStream` declares a single-owner invariant that one of its own methods breaks.** The field comment says only `reader`/`cleanup` are shared; `Close` honours it via `Stop → closeCurrent → Wait`; `ReopenAfterTransportFailure` calls `closeCurrent + Open` from a foreign goroutine with neither. → M-08.

**RC-5 — The execution model was inherited from a library default, not chosen.** Command bodies run inline on disgo's single read goroutine; `COMMAND_PARALLELISM=16` documents a property the runtime does not have. → M-05, and M-09's blast radius.

---

# 2. Target architecture

```
PROCESS LIFETIME
────────────────────────────────────────────────────────────────────────
  Bot
   ├── storage                                           process
   ├── voiceResources() → (VoiceManager, *DaveRegistry, ok)   ← resolver,
   │      reads b.currentConn() atomically; the ONLY way          not a value
   │      anything below reaches the live session
   └── voice.Service                                      process
        ├── resolver (resolve.Resolver)                    process, read-only
        ├── sinkProviders[guild]  *sink.Provider           process ← NOW CORRECT
        │      holds: guildID, voiceReadyDelay, log, voiceResources
        │      holds NO manager, NO dave registry, NO conn state
        └── Player[guild]                                  process
             │ OWNS: queue, currTrack (the only Track the UI sees),
             │       playing/starting, run generation      [guard: p.mu]
             │ RULE: no field here has session lifetime
             ├── Queue                                     player
             └── Run(gen N)                                track
                  └── RecoveryStream                       run
                       │ OWNS: its OWN private Track copy, retries,
                       │       parserIndex, seekSec, curParser, firstRead,
                       │       fromCache, cacheDisabled, cacheWriter
                       │ RULE: mutated by exactly ONE goroutine —
                       │       whoever drives ReadPacket
                       └── BufferedReader producer (or the sink goroutine
                           when BUFFER_AHEAD_MS=0 — both converge on one owner)

════════════ THE SEAM: pkg/music/sink ═══════════════════════════════════
  Provider.Sink(target) (AudioSink, error)   — acquire
  AudioSink.Stream(r, stop) error            — send; blocks; reports failure
  Provider.ReleaseSink / InvalidateSink      — release
  CONTRACT ADDS (doc + tests, no new type):
    · a sink MUST return stream.ErrVoiceTransport when its transport dies
    · a sink MUST NOT emit a frame the transport cannot protect
═════════════════════════════════════════════════════════════════════════

SESSION LIFETIME  (one per RunSession; everything here dies together)
────────────────────────────────────────────────────────────────────────
  Session → bot.Client → VoiceManager   ← AUTHORITATIVE for conn liveness
          → DaveRegistry
                 └── voice.Conn         ← disgo-owned, disposable
                        └── DAVE Session ← per conn; consulted PER FRAME

GATEWAY BOUNDARY
────────────────────────────────────────────────────────────────────────
  disgo listen goroutine (stays SERIAL — Ready/GuildJoin ordering is
  load-bearing).  Handlers mark, route, and return.
        │
        └── per-guild command worker (global cap: execguard)
                 command body: REST, resolve, stream open — off the loop
```

Dependency direction is unchanged: `internal/discord → pkg/music`, never back. Six rules, all grep-checkable:

1. No field reachable from a `Player` has session lifetime.
2. `RecoveryStream` state is mutated by one goroutine; reopen is a *request*.
3. `RecoveryStream` never writes the caller's `Track`; facts travel up as values.
4. `p.mu` is never held across I/O.
5. Connection liveness is answered by `VoiceManager`, never by a melodix field.
6. The gateway read loop dispatches and returns.

---

# 3. Refactor map

| Entity | Current owner | Current lifetime | Desired owner | Desired lifetime | Required change |
|---|---|---|---|---|---|
| **Bot** | `main` | process | same | same | — |
| **storage.Storage** | Bot | process | same | same | — |
| **voice.Service** | Bot | process | same | same | rename later (P3); no structural change |
| **resolve.Resolver** | Service (`s.mu`, lazy) | process | same | same | — (read-only after build) |
| **Player** | Service `players[guild]` | process | same | same | — |
| **Queue (`p.queue`)** | Player, `p.mu` | player | same | same | — |
| **`p.currTrack`** | Player, `p.mu` | run | **Player, sole writer** | run | **stop RecoveryStream/parsers writing it**; apply `OpenInfo` under `p.mu` |
| **Track (exported)** | pointer escapes via `CurrentTrack()` | — | value only | — | `CurrentTrack() parsers.Track` |
| **Track (inside stream)** | shared with Player + parsers | — | **RecoveryStream private copy** | run | RS copies on construct; parsers mutate the copy |
| **run generation** | implicit (`currTrack` pointer identity) | run | **explicit `gen int64`** under `p.mu` | run | add field; `Stop`/`clearIfCurrent` compare it |
| **`stopPlayback`/`playbackDone`/`stopOnce`** | Player, `p.mu` | run | same | same | `Stop` phase 2 resets only if gen unchanged |
| **RecoveryStream run state** (retries, parserIndex, seekSec, curParser, firstRead, fromCache, cacheDisabled, cacheWriter) | *two* goroutines | run | **the ReadPacket driver, exclusively** | run | `ReopenAfterTransportFailure` → `RequestReopen()` flag + `closeCurrent()`; the driver performs it |
| **`rs.reader`/`rs.cleanup`** | `rs.mu` | run | same | same | — (already correct) |
| **BufferedReader** | RecoveryStream | run | same | same | — |
| **sink.Provider** | *session* (`clientConn`) then cached at process scope | **mismatched** | **Bot, process** | process | drop `manager`/`dave` fields; take `voiceResources()` resolver |
| **`Provider.conn` / `currentChannelID`** | Provider mirror | — | **VoiceManager (authoritative)** | voice-conn | delete the state mirror; keep conn **identity tag** + our own target channel |
| **AudioSink (`Sink`)** | per acquisition | per acquisition | same | same | add pull-liveness watchdog + DAVE gate |
| **VoiceManager** | bot.Client | session | same | same | — (now resolved, never retained) |
| **DaveRegistry** | Session (`RunSession`) | session | same | same | — (now resolved, never retained) |
| **voice.Conn** | VoiceManager | voice-conn | same | same | — |
| **DAVE Session** | voice.Conn | voice-conn | same | same | — (gate moves to the send path) |
| **`deadSinkProvider`** | `internal/discord` | — | **deleted** | — | the resolver's "no session" answer subsumes it |
| **`conn.NewSinkProvider`** | `clientConn` | session | **deleted** | — | providers no longer built per session |
| **`Service.sinkProviders`** | Service | process | same | same | cache becomes *correct*; keep it |
| **command execution** | gateway goroutine | inline | **per-guild worker** | per command | Phase 6 |
| **`cmdsync.perGuildLocks`** | Syncer | session | **deleted** | — | callers stay on the serial loop; the lock is provably unreachable |
| **`execguard.Guard`** | Bot (atomic) | session | same | same | becomes load-bearing in Phase 6 |
| **`Session.lastHeartbeatAck`** | `s.mu` RWMutex | session | same | same | — (correct) |
| **`Tracker`** | atomics | session | same | same | — (correct) |
| **`guildMusicStatus` / notify** | Service, `guildMusicStatusMu` | process | same | same | — (correct) |
| **cache.Store** | `s.mu`, I/O outside | process | same | same | — (correct; use as the reference pattern) |
| **visitor / soundcloud clientID** | package mutex, I/O *inside* | process | same | same | — **accepted**: the lock exists to collapse a stampede |

---

# 4. Findings disposition

| ID | Root cause | Architectural? | Disposition | Exact refactor |
|---|---|---|---|---|
| **M-01** DAVE not gated on send | RC-3 | **contract, not structure** | **NEEDS SEPARATE FIX** (local, transport layer) | `Sink.Stream` captures the guild's `*davesession.Session`; `frameProvider.ProvideOpusFrame` returns a zero-length frame while `ShouldHoldFrames()`, bounded by `daveReadyTimeout` then `finish(ErrVoiceTransport)` |
| **M-02** transport death invisible | RC-3 | **contract + detection** | **ELIMINATED BY REFACTOR** (Phase 5) — *partially*: see note | `Sink.Stream` gains two watchdog cases: (a) conn identity lost (`GetConn(gid) != openedConn`), (b) **pull-liveness** — no `ProvideOpusFrame` call for > K frames ⇒ `ErrVoiceTransport` |
| **M-02b** generic UDP error drains silently | disgo swallows it | no | **UNAFFECTED — not solvable in melodix** | disgo's `handleErr` logs and continues; melodix cannot observe it. Mitigate by classifying `"failed to send audio"` in the existing `logWriter` slog bridge and counting it. Upstream fix belongs in disgo. **Say this out loud rather than pretend.** |
| **M-03** provider bound to dead session | RC-2 | **yes** | **ELIMINATED BY REFACTOR** | `Provider` stops holding `manager`/`dave`; takes `voiceResources()` and resolves per call. The stale state ceases to exist. |
| **M-04** `Stop` clobbers a newer run | ownership | partly | **WAS AN UNPROVEN FINDING** — 47 rounds, 0 reproductions; `startTrack`'s late `stopCh` read is self-healing | Still worth the guard: explicit `gen` + `Stop` resets only if `gen` unchanged. Cheap, and a **prerequisite for Phase 6**, which widens the window |
| **M-05** inline command execution | RC-5 | **yes** | **ELIMINATED BY REFACTOR** (Phase 6) | `runGuardedInteraction` hands the body to a per-guild worker; gateway loop stays serial |
| **M-06** mirrored conn state | RC-2 | **yes** | **ELIMINATED BY REFACTOR** — *same change as M-03* | delete `p.conn` as a state mirror; keep an identity tag; ask `manager.GetConn(gid)` |
| **M-07** Track race | RC-1 (**S-1**) | **yes**, and larger than reported | **ELIMINATED BY REFACTOR** | RecoveryStream owns a private Track copy; `Open` returns `OpenInfo`; the callback carries `OpenInfo`; Player applies it under `p.mu`; `CurrentTrack()` returns a value |
| **M-08** RecoveryStream race | RC-4 | **yes** | **ELIMINATED BY REFACTOR** | `ReopenAfterTransportFailure()` → `RequestReopen()`: set flag + `closeCurrent()`; the ReadPacket driver performs the reopen |
| **M-09** I/O under `p.mu` | local slip | **no** | **NEEDS SEPARATE FIX** | capture `target`+provider under `p.mu`, unlock, then `ReleaseSink` |
| **S-1** (new) `Streamer.Open` out-parameter | RC-1 | **yes** | **REDUCED BY REFACTOR** (contained, not removed) | The out-parameter stays, but it now writes RecoveryStream's private copy on the same goroutine. Changing the interface across 5 packages is optional future work, not required for correctness |

**One important admission:** M-02 is eliminated for the *wedge* (the proven, damaging failure) and **not** for the silent-drain variant. No architecture inside melodix can observe a UDP error that disgo logs and discards. Claiming otherwise would be exactly the kind of overreach this exercise is meant to avoid.

---

# 5. Structural duplicates → one refactor each

| Bugs | One root cause | One refactor |
|---|---|---|
| M-03 + M-06 | cached pointer to a shorter-lived resource, inside `sink.Provider` | **delete both caches**; resolve manager/dave per call, ask the manager for conn liveness |
| M-07 + S-1 | mutable Track shared downward as an out-parameter | **RecoveryStream owns a private copy; facts travel up as values** |
| M-08 + M-04 | two goroutines mutating one logical run's state | **one owner per run**: RecoveryStream's driver; Player's generation |
| M-01 + M-02 | the sink contract defines only success | **write the two clauses; implement both in `Sink.Stream`/`frameProvider`** |

Four refactors, ten findings.

---

# 6. Radical alternatives — decisions

| Idea | Problem it solves | Needed? | Can current arch express it? | Cost | Decision |
|---|---|---|---|---|---|
| **Player actor/mailbox** | cross-goroutine Player mutation | no — M-04 unproven | yes: explicit `gen` + "no I/O under `p.mu`" | rewrite of the repo's trickiest 730-line file | **REJECT** — and note it touches *neither* M-07 nor M-08, the two proven races |
| **Voice actor** | voice state races | no | yes: disgo already owns conn state; we stop mirroring | new goroutine + mailbox per guild | **REJECT** |
| **VoiceSupervisor** | reconnect orchestration | no | yes: `runPlayback`'s existing 3-attempt loop + a working failure signal | a supervision tree for one retry loop | **REJECT** |
| **PlaybackTask / structured concurrency** | run lifetime clarity | partly | yes: the run already owns `stopCh`/`doneCh`/`rs`; adding `gen` completes it | medium | **PARTIALLY ADOPT** — take the *idea* (explicit generation), not the machinery |
| **Explicit `VoiceTransport` type** | transport boundary | **no — it already exists** | `sink.Provider` + `AudioSink` are exactly this shape | a third name for `Provider` in a repo that already has two `sink` packages | **REJECT the type, ADOPT the semantics** (contract clauses) |
| **State-machine DAVE lifecycle** | epoch transitions | no | dave-go already implements the state machine; melodix needs one `if` on the send path | duplicating a 4,500-line library | **REJECT** |
| **Immutable Track** | M-07 | no | parsers legitimately fill Title/Duration at open; threading new values back through `packetView`→`BufferedReader`→`Sink` means redesigning the packet path | high | **REJECT** — private copy + `OpenInfo` gets the invariant at a fraction of the cost |
| **Per-guild command workers** | M-05 without losing ordering | **yes** | not today — nothing serializes per guild | ~30 lines | **ADOPT** |
| **Global async dispatch** | M-05 | no | — | 1 line, but spawns 6 goroutines per listener per event under `IntentsAll`, unbounded | **REJECT** |
| **Rust/Tokio ownership** | all of it | no | — | — | **REJECT** |
| **Interfaces "for testability"** | test seams | no | `stream.SetRegistry`, `SinkProviderFactory`, `APIGetter` already exist | — | **REJECT** |

---

# 7. Implementation phases

---

### Phase 0 — Failing-test baseline

**Goal:** turn the three verified defects into red tests before any production change.

**Affected:** new `internal/discord/voice/sink/*_test.go`, `pkg/music/stream/recovery_concurrency_test.go`.

**Structural change:** none.

**Specific changes:**
- `sink_transport_test.go` — stub `voice.Conn`/`voice.UDPConn` driving the **real** `voice.NewAudioSender`; `Write` returns `net.ErrClosed`; assert `Sink.Stream` returns `ErrVoiceTransport` within ~1 s. **RED.**
- `dave_hold_test.go` — fake `godave.Session` with flippable `Ready()`/`ShouldHoldFrames()`; assert `ProvideOpusFrame` yields no audible frame while holding. Port the four deleted fork cases. **RED.**
- `recovery_concurrency_test.go` — `Packets()` + a consumer + concurrent `ReopenAfterTransportFailure`, under `-race`. **RED** (27 races incl. a `retries` map read/write).

**Invariants established:** none yet — these *are* the invariants, written down.

**Bugs fixed:** none. **New tests:** the three above. **Risk:** none. **Independently mergeable:** yes, if CI tolerates skipped/`t.Skip`-guarded reds; otherwise merge each with its phase.

---

### Phase 1 — DAVE frame gating *(local, transport layer)*

**Goal:** a sink never emits a frame the transport cannot protect.

**Affected:** `internal/discord/voice/sink/sink.go`, `provider.go`.

**Structural change:** none. The gate moves from *join-time* to *send-time*, inside the layer that already owns DAVE.

**Specific changes:**
- `Sink` gains `dave *davesession.Session`, captured in `Provider.Sink` via `p.dave.Session(p.guildID)` — on **both** branches, including the cached-conn fast path that currently skips `awaitEncryption` entirely.
- `frameProvider` gains `dave` + a `holdSince time.Time`. At the top of `ProvideOpusFrame`: if `dave != nil && dave.ShouldHoldFrames()` → return `nil, nil` (disgo's sender emits `SilenceAudioFrame` ×5 then stops speaking — verified correct hold behaviour) and record the hold start; when the hold exceeds `daveReadyTimeout`, `finish(ErrVoiceTransport)` and return `nil, io.EOF` so a hold cannot become a wedge.
- Keep `awaitEncryption` for the initial join (it usefully fails a join into an E2EE channel that never comes up), but it is no longer the safety mechanism.
- Write the contract clause into `pkg/music/sink/sink.go`'s `AudioSink` doc.

**Invariants:** Invariant 5 (protocol safety owned by the transport layer).
**Bugs fixed:** **M-01.**
**New tests:** Phase 0's `dave_hold_test.go` goes green; add "a hold that never resolves ends the track rather than wedging".
**Risk:** low — gating too eagerly causes silence, which the bounded hold converts to a recoverable error. **Independently mergeable:** yes.

---

### Phase 2 — Track ownership *(S-1 / M-07)*

**Goal:** exactly one writer for the Track the UI sees.

**Affected:** `pkg/music/stream/recovery.go`, `pkg/music/player/player.go`, `internal/discord/voice/service.go`, `internal/discord/reply/musicstatus.go`, `cmd/cli/main.go`.

**Structural change:** `RecoveryStream` stops writing its caller's state. Runtime facts travel **up** as values, through the callback that already exists.

**Specific changes:**
- New value type in `pkg/music/stream` (or `parsers`): `OpenInfo{Parser string; Passthrough, Cached bool; Title, Artist string; Duration time.Duration}`.
- `NewRecoveryStream*` **copies** the Track: `rs.track = cloneTrack(*track)`. Parsers keep their out-parameter and now mutate RecoveryStream's private copy on the goroutine that called them.
- `rs.Open(seek) (OpenInfo, error)`; `SetOnParserConfirmed(func(OpenInfo))`.
- `Player.startTrack` applies the returned `OpenInfo` to `p.currTrack` under `p.mu` **before** `emitStatus(StatusPlaying)` — which also fixes a real cosmetic bug today, where the first "Now Playing" names the *preference* rather than the parser that opened.
- `Player.onParserConfirmed(track, info)` applies the update under `p.mu` before recording history, preserving today's ordering.
- **`CurrentTrack() parsers.Track`** (value). Update 4 external call sites: `playback.StartAndRender`, `next.go`, `voice.Service.watchPlayerStatus`, `cmd/cli/main.go` — all currently nil-check a pointer; give them `(parsers.Track, bool)` or keep `*Track` pointing at a fresh clone to minimise churn. Prefer `(Track, bool)`.

**Invariants:** 1, 3, 4 (Invariant 4 in the task brief).
**Bugs fixed:** **M-07** eliminated; **S-1** contained.
**New tests:** status-watcher-vs-parser-switch under `-race` (the Phase 0 probe shape); "a mid-track parser switch updates the chip exactly once"; "the first Now Playing names the parser that actually opened".
**Risk:** medium — touches the history-recording path. Mitigated by `music_history_test.go` and by keeping the callback ordering identical. **Independently mergeable:** yes.

---

### Phase 3 — RecoveryStream single-owner *(M-08)*

**Goal:** one goroutine mutates a run's stream state. **Must precede Phase 5.**

**Affected:** `pkg/music/stream/recovery.go`, `pkg/music/player/player.go` (one call site).

**Structural change:** reopen becomes a **request**, serviced by whoever drives `ReadPacket`.

**Specific changes:**
- `ReopenAfterTransportFailure() error` → `RequestReopen()`. It sets a flag (an `atomic.Bool`, or a field under the existing `rs.mu`) and calls `closeCurrent()` — which already exists precisely to unblock a producer parked in a source read.
- `ReadPacket`'s loop checks the flag at the top and, if set, clears it and performs `Open(rs.seekSec)` itself. `seekSec` is already the *producer's* position, so resume semantics are unchanged and the buffered lead still plays.
- The open error no longer returns synchronously — it surfaces as a read error on the next `Stream`, which `runPlayback` already handles. **`player.go:591`'s `reopenErr` branch deletes.**
- Both configurations converge: with `BUFFER_AHEAD_MS=0` the driver *is* the run goroutine, so the flag round-trip is a no-op.

**Invariants:** 1, 3.
**Bugs fixed:** **M-08** eliminated (all 27 races, including the `retries` map read/write that is a `fatal error` in a non-race build).
**New tests:** Phase 0's race test goes green; "reopen during a full buffer completes once the sink resumes"; "Close during a pending reopen does not deadlock"; "three failure/recovery cycles preserve position".
**Risk:** medium — this is the recovery hot path. Mitigated by `recovery_test.go` (519 lines) and by the fact that the *sequence* of operations is unchanged, only the goroutine performing them. **Independently mergeable:** yes (the path is currently unreachable, so this ships as pure hardening).

---

### Phase 4 — Player lock hygiene and run identity *(M-09, M-04)*

**Goal:** locks protect state, not I/O; a run's state is reset only by its own generation.

**Affected:** `pkg/music/player/player.go` only.

**Structural change:** none — two local corrections that make existing intent enforceable.

**Specific changes:**
- Add `gen int64` to `Player`, incremented in `startTrack` under `p.mu`, captured by `runPlayback`, compared in `clearIfCurrent` (replacing pointer identity — which also removes the last internal dependency on `CurrentTrack()`'s pointer, completing Phase 2).
- `Stop` phase 1 captures `gen`; phase 2 resets `playing/starting/currTrack` and mints channels **only if `p.gen` is unchanged**.
- `Stop` phase 2: capture `target` and the provider under `p.mu`, **unlock**, then call `ReleaseSink`. Model it on `cache.Store.register`, which already does exactly this and says why.
- Add the package-doc rule: *"`p.mu` is never held across I/O."*

**Invariants:** 1, 6.
**Bugs fixed:** **M-09** eliminated; **M-04** guarded (prerequisite for Phase 6).
**New tests:** "a `Stop` whose `ReleaseSink` blocks 5 s does not block `Queue()`/`IsPlaying()`"; extend the existing concurrent hammer with `Stop`/`PlayNext`/completion-chain interleavings.
**Risk:** low. **Independently mergeable:** yes.

---

### Phase 5 — Transport ownership *(M-03, M-06, M-02)*

**Goal:** the transport layer holds no stale references and reports its own death. **Depends on Phase 3.**

**Affected:** `internal/discord/voice/sink/provider.go`, `sink.go`, `internal/discord/conn.go`, `session_run.go`, `session_bootstrap.go`, `pkg/music/sink/sink.go` (doc only).

**Structural change:** the provider becomes process-lifetime and resolves session resources; the conn state mirror is deleted.

**Specific changes:**
- `Bot` exposes `voiceResources() (voice.Manager, *sink.DaveRegistry, bool)` reading `b.currentConn()` atomically. `clientConn` keeps `client` + `dave`; **`conn.NewSinkProvider` and `deadSinkProvider` are deleted** (two concepts removed).
- `Bot.newSinkProvider(guildID)` builds a process-lifetime `*sink.Provider{guildID, voiceReadyDelay, log, resolve: b.voiceResources}`. `Service.sinkProviders` keeps memoizing — and is now *correct*.
- `Provider` **loses** `manager` and `dave` fields. It keeps `guildID`, `voiceReadyDelay`, `log`, plus `openedConn voice.Conn` (an **identity tag**, never dereferenced for protocol state) and `currentChannelID string` (which melodix legitimately owns — it is what melodix asked for).
- `Provider.Sink(target)`: `mgr, dave, ok := p.resolve()`; `!ok` → `errNoConnection`. Reuse only when `mgr.GetConn(p.guildID) == p.openedConn && p.currentChannelID == target`. Any mismatch — new manager after a restart, conn removed by `handleGatewayClose` — falls through to a fresh `CreateConn`. **M-03 and M-06 both become unrepresentable.** Deliberately compare pointer identity rather than reading `conn.ChannelID()`, which disgo writes without a lock.
- `Sink.Stream` gains two watchdog cases in its `select`:
  1. **pull-liveness** — `frameProvider` stamps `lastPull` (atomic) on every `ProvideOpusFrame`; if no pull for > ~25 frames (500 ms) → `ErrVoiceTransport`. Catches the proven `net.ErrClosed` wedge and the `HandleVoiceStateUpdate(nil)` kick. This is the pull-model analogue of the deleted `TestSendOpusTimeoutIsVoiceTransport`.
  2. **conn identity** — `mgr.GetConn(guildID) != openedConn` → `ErrVoiceTransport`. Catches session restart and external removal fast.
- Delete `frameProvider.Close`'s role as *the* failure signal (keep the method — it satisfies the interface — but it is no longer load-bearing).
- Write the failure clause into `AudioSink`'s doc.
- **Log-bridge mitigation for M-02b:** classify disgo's `"failed to send audio"` in `session.logWriter` and emit it as a distinct, countable zerolog event. Explicitly labelled a mitigation, not a fix.

**Invariants:** 2, 5, 6 (of the target-architecture list).
**Bugs fixed:** **M-03, M-06** eliminated; **M-02** eliminated for the wedge; **M-02b** surfaced only.
**New tests:** Phase 0's transport test goes green; "a provider survives a simulated session swap and uses the new manager"; "a conn removed behind the provider's back forces a rejoin, not a reuse"; "the guild's DAVE session is looked up from the current registry after a swap".
**Risk:** **highest in the plan.** It is the voice path. Mitigated by landing Phase 3 first, by the Phase 0 harness, and by a manual smoke run (join, play, kick the bot mid-track, verify auto-recovery).
**Independently mergeable:** yes, but ship it alone — do not bundle.

---

### Phase 6 — Execution model *(M-05)*

**Goal:** command bodies leave the gateway goroutine without losing ordering. **Depends on Phase 4.**

**Affected:** `internal/discord/handlers_common.go`, `handlers.go`, `cmdsync/syncer.go`, `config.go`, `session/session.go`.

**Structural change:** a per-guild command worker between dispatch and execution.

**Specific changes:**
- `runGuardedInteraction` hands the body to a worker keyed by guild (a small `map[string]chan func()` with one goroutine each, or a per-guild mutex acquired on a spawned goroutine — whichever reviews more simply). The gateway listener returns immediately. `execguard` caps global concurrency and finally does something.
- `onReady`/`onGuildJoin` stay **inline** on the serial loop — their ordering is load-bearing. Consequently `cmdsync.perGuildLocks` becomes provably unreachable: **delete it** (one concept fewer).
- Delete `cmdsync`'s `time.Sleep(rateLimitDelay)` ×3 — disgo's REST client rate-limits.
- Resolve `COMMAND_TIMEOUT`: either thread `ctx` into the four call sites that could honour it, or delete the knob. **Not a third option.**
- Narrow `gateway.IntentsAll` → `IntentGuilds | IntentGuildVoiceStates` (+ `IntentGuildMessages` only if the mention path survives) and `cache.FlagsAll` → `FlagGuilds | FlagChannels | FlagRoles | FlagMembers | FlagVoiceStates`. This is what makes the event volume small enough for the rest to matter, and removes the privileged-intent 4014 risk.
- Shutdown: workers drain under the existing `closeWithin` budget.

**Invariants:** 7 (execution model is intentional).
**Bugs fixed:** **M-05** eliminated; **M-09**'s blast radius reduced from bot-wide to guild-local.
**New tests:** "two guilds running a 2 s command overlap"; "two commands in one guild do not overlap"; "a slow command does not delay `MarkWSNow` for another event"; "`COMMAND_PARALLELISM=1` serializes across guilds".
**Risk:** medium — ordering semantics change. Mitigated by keeping the loop serial and by per-guild serialization.
**Independently mergeable:** yes.

---

### Phase 7 — Cleanup and re-audit

`atomic.Pointer` for the three holder structs (+ delete `bot_atomic_test.go`, which tests the standard library); `rest.JSONErrorCodeInteractionAlreadyAcknowledged` instead of substring matching; delete the dead `onMessageCreate` path and `-check-disgo`; correct `docs/architecture.md` (it still claims a deleted test exists) and `docs/dave-todo.md` (it says the recovery timeout is unset; `dave_registry.go` sets 5 s). The `sink` package rename is its own commit, later. Then re-run the forensic pass plus one live E2EE run watching `dave.State().Stats.PassthroughFrames`.

---

# 8. Tests per invariant

| Invariant | Test | Phase |
|---|---|---|
| A sink never emits an unprotected frame | fake `godave.Session`, flip `ShouldHoldFrames` mid-stream, assert no audible frame; assert a permanent hold ends the track rather than wedging | 1 |
| RecoveryStream never writes the caller's Track | after a mid-track parser switch, the Track handed to `NewRecoveryStream` is byte-identical | 2 |
| The UI never shares engine state | status watcher + `CurrentTrack()` hammer vs parser switch, `-race` | 2 |
| One goroutine mutates stream state | `Packets()` + consumer + concurrent `RequestReopen`, `-race`; close-during-pending-reopen; 3 failure/recovery cycles preserve position | 3 |
| `p.mu` is never held across I/O | a provider whose `ReleaseSink` blocks 5 s; assert `Queue()`/`IsPlaying()` stay responsive | 4 |
| Only a run's own generation resets its state | `Stop` vs completion-chain vs `/next` interleavings; assert no live run after `Stop(true)`; assert never two live runs | 4 |
| A long-lived provider never retains a dead session | swap the resolver's answer; assert the same `*Provider` uses the new manager and the new DAVE registry | 5 |
| VoiceManager is authoritative | remove the conn behind the provider; assert rejoin, not reuse | 5 |
| Transport death reaches the player | real `AudioSender` + stub conn, `Write → net.ErrClosed`; assert `ErrVoiceTransport` ≤ 1 s; then assert `runPlayback` reopens and resumes | 5 |
| Session restart during playback recovers | simulate restart mid-track; assert rejoin on the new session and continued playback | 5 |
| Gateway stays responsive | two guilds × 2 s commands overlap; a slow command does not delay another event's handling | 6 |
| Per-guild ordering holds | two commands in one guild never overlap | 6 |

Everything touching `player`, `stream` or `sink` runs under `-race` in CI, which the project already requires.

---

# 9. Explicitly rejected

Not introducing, and why: **an actor framework or mailbox Player** (touches neither proven race; rewrites the riskiest file); **a `VoiceTransport` type** (`sink.Provider` + `AudioSink` already are it — a third name in a repo with two `sink` packages is a net loss); **an event bus, DI container, or lifecycle framework** (no finding is caused by their absence); **global async dispatch** (unbounded goroutines per listener per event); **immutable Track / immutable everything** (a private copy plus `OpenInfo` buys the invariant far cheaper); **a DAVE state machine** (dave-go is one); **channels replacing mutexes** (`p.mu`, `playNextMu`, `rs.mu` are all correct for what they cover — the bugs are scope and ownership); **interfaces for testability** (`SetRegistry`, `SinkProviderFactory`, `APIGetter` already exist); **package splits for tidiness** (including the `sink` rename, which is real but must not ride along in a concurrency diff); **changing the `Streamer` interface across five parser packages** (contained in Phase 2, revisit only if it bites again); **rewriting the music engine**; **changing libraries** — nothing here shows disgo or dave-go prevents the required invariants; disgo's gap (never calling `OpusFrameProvider.Close`) is worked around at melodix's own boundary and is worth an upstream PR, not a migration.

Net concept count: **−4** (`deadSinkProvider`, `conn.NewSinkProvider`, `cmdsync.perGuildLocks`, `bot_atomic_test.go`), **+2** (`OpenInfo`, per-guild worker). Two fewer moving parts than today.

---

# 10. First implementation task

> **Gate the audio send path on DAVE readiness (M-01), with tests.**

**Scope — one commit:**

- `internal/discord/voice/sink/sink.go` — `Sink` carries the guild's `*davesession.Session`; `frameProvider` consults `ShouldHoldFrames()` on every `ProvideOpusFrame`, returning `nil, nil` while holding, and `finish(stream.ErrVoiceTransport)` + `nil, io.EOF` once a hold exceeds `daveReadyTimeout`.
- `internal/discord/voice/sink/provider.go` — capture `p.dave.Session(p.guildID)` into the returned `Sink` on **both** branches, including the cached-conn fast path that currently bypasses `awaitEncryption` for tracks 2..N.
- `pkg/music/sink/sink.go` — add one sentence to `AudioSink`'s doc: *a sink must not emit a frame its transport cannot protect.* This is the clause whose absence let the property vanish in the migration.
- `internal/discord/voice/sink/dave_hold_test.go` — a fake `godave.Session` with flippable readiness, plus the four cases ported from the deleted fork test: no-encryption channel sends; no-epoch holds; live epoch sends; a hold that never resolves ends the track instead of wedging.

**Why this one first:** it is the only P0 that is fully independent of every architectural decision in this plan — correct under all seven phases and under every alternative rejected in §9. It restores a security property the project had already built, tested and shipped. It touches two files and adds one test file. It creates the fake `godave.Session` fixture that Phase 5 reuses. And it is reviewable in a sitting.

**Do not** bundle the transport watchdog, the provider-lifetime change, or `awaitEncryption`'s removal into this commit. `awaitEncryption` stays exactly as it is — it becomes redundant as a safety mechanism but remains useful as a join-time check, and removing it belongs to Phase 5.

**Definition of done:** `dave_hold_test.go` green; `go test -race ./...` green; `docs/architecture.md`'s Discord-sink paragraph updated to state the hold rule.