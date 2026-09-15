I'll verify each finding against actual source. Starting with dependency pinning and the exact modules in use.

Repository unmodified (the only untracked file, `docs/audit_2026-09-15_claim.md`, was not created by me). All experiments ran in the scratchpad as separate modules. Findings below are from the pinned build list: `disgo v0.19.6`, `godave v0.2.0`, `dave-go v0.5.1`, no `replace` directives, `go mod verify` clean.

---

# 1. M-01 — DAVE re-key / plaintext audio

## A. Where is DAVE readiness checked?

Exhaustive grep for `Ready()` / `ShouldHoldFrames` / `IsReady` / `holdFrames` across all three modules:

- **melodix**: exactly one site — [provider.go:115](internal/discord/voice/sink/provider.go:115), `if dave == nil || !dave.ShouldHoldFrames()` inside `awaitEncryption`. Its only caller is `Provider.Sink`, and only on the join branch. (`watchdog.IsReady` is gateway readiness, unrelated.)
- **disgo `voice/`**: **zero** matches. The whole package never calls `Ready()`.
- **godave**: declares `Ready()` on the interface; only the noop session implements a call.

So readiness is checked **once per voice join**, never per frame, never in the send loop. Worse, `Provider.Sink`'s fast path returns before `awaitEncryption` runs at all:

```go
if p.conn != nil && p.currentChannelID == target {
    return &Sink{conn: p.conn, log: p.log}, nil   // provider.go:68-70 — no gate
}
```

Tracks 2..N of a queue are therefore never gated even at join granularity.

## B. What does dave-go do when encryption is not ready?

`Session.Encrypt` ([session.go:337](file)) does not error, does not block, and has no hold queue:

```go
ratchet := s.selectSendRatchetLocked()
if ratchet == nil {
    s.stats.PassthroughFrames++
    return copy(encryptedFrame, frameData), nil     // session.go:352-354
}
```

Passthrough is intentional and documented (`godave.Session.Ready` doc: *"Encrypt still forwards frames unmodified (passthrough) rather than erroring, so callers that don't gate on Ready continue to work"*). The contract is explicit that the **caller** must hold.

Critically, `selectSendRatchetLocked` returns `retainedSendRatchet` while it is within `sendRetentionTTL` — which is `epochRetention = 10 * time.Second`. That narrows the exposure substantially and the previous audit missed it.

## C. What actually happens during MLS re-key?

I traced every write to `sendRatchet` / `retainedSendRatchet` / `activeEpoch` in non-test code. There are four paths, and they are **not** equivalent:

**Path 1 — the ordinary membership change (`OnDaveExecuteTransition` → `activatePendingEpochLocked`, mls.go:724).** This is the path a user joining or leaving takes. It is **atomic under `s.mu`**: `s.activeEpoch = s.pendingEpoch` and `s.sendRatchet = sender.ratchet` happen in one locked call, and `Encrypt` takes the same `s.mu`. There is no observable window in which `sendRatchet` is nil. When the new epoch *adds* a member, retention is deliberately skipped and the ratchet switches immediately. **The previous audit's claim that plaintext escapes on "every re-key" is wrong.**

**Path 2 — `OnDavePrepareEpoch(epoch=1)` (session.go:656).** `activeEpoch = nil`, `sendRatchet = nil`, `retainedSendRatchet = old` with a 10 s TTL. For 10 s, frames are still encrypted — but under the *previous* epoch's key. After 10 s, if no Welcome has activated a new epoch, `selectSendRatchetLocked()` returns nil and **every subsequent frame is passthrough**.

**Path 3 — `OnSelectProtocolAck(pv > 0)` (session.go:547).** Same shape, at (re)connection. `awaitEncryption` covers the first join only.

**Path 4 — degraded activation (mls.go:783).** `else { s.sendRatchet = nil; s.markDegradedLocked("epoch activated but self not in senders map") }`. Same 10 s grace, then passthrough.

Downgrade to protocol v0 (session.go:635) also clears everything, but that is **correct** — `ShouldHoldFrames()` returns `!ready && protocolVersion != 0`, so it is false there and plaintext is what belongs on the wire.

Melodix's `frameProvider.ProvideOpusFrame` has no gate whatsoever, and disgo's voice gateway (gateway.go:573-610) forwards DAVE opcodes into the session and does nothing else — it never pauses the sender, never changes speaking state, never consults `Ready()`. So yes: melodix keeps producing frames throughout every one of those windows.

## D. The failure, minimally

```
t0   E2EE channel, epoch live, frames encrypting normally
t1   gateway sends prepare_epoch(epoch=1)  [protocol bump / group rebuild]
       -> dave-go: activeEpoch=nil, sendRatchet=nil,
          retainedSendRatchet=old, expires t1+10s
       -> Ready()=false, ShouldHoldFrames()=true   (nobody asks)
t1..t1+10s   frames encrypt under an epoch the group has left
             => receivers drop them => silence
t1+10s   retained ratchet expires; no Welcome has arrived
t1+10s+  selectSendRatchetLocked() == nil
         EVERY frame: dave-go/session/session.go:354
             return copy(encryptedFrame, frameData), nil
         then disgo/voice/udp_conn.go:280
             conn.Write(u.encrypter.Encrypt(u.header, u.encryptBuffer[:n]))
```

`udp_conn.go:280` is the exact line where the frame leaves the process. To be precise: it is not plaintext on the wire — the *transport* cipher still applies — but the **end-to-end layer is absent** on a channel Discord has marked E2EE. Discord's infrastructure can read it and E2EE-expecting receivers discard it.

## E. Historical evidence

The document is corroborated by the code, and the fork's mechanism was stronger than the document describes. `git show 5953df1^:pkg/discordgo-fork-dev/dave_inject.go`:

```go
func holdFrames(dave godave.Session) bool {
    return dave != nil && !dave.Ready()
}
```

called **per frame** at `voice.go:1205`, with a second guard at `voice.go:1233` that refused to send when `Encrypt` errored, and four tests pinning it (`dave_hold_test.go`: `TestHoldFramesAllowsAChannelWithoutEncryption`, `...StopsSendingWithoutAnEpoch`, `...SendsOnceAnEpochIsLive`, `TestUnconfiguredSessionHoldsEverything`). Three commits are titled *"voice: hold frames instead of sending them unencrypted"* (cd810fc, 75ef964, cc2ab4b), plus a6c8dff *"voice: stop sending under an epoch the group has left"*. All deleted in 5953df1 with nothing equivalent on the disgo path.

One nuance in melodix's favour: the *predicate* was translated correctly. The fork used `dave != nil` as "is this channel encrypted", because it only built `v.dave` when op4 reported version > 0. disgo always builds a session, so `protocolVersion != 0` is the right test — which is precisely what `ShouldHoldFrames()` encodes. The translation of the predicate is sound. What was lost is the **call site**: per-frame became once-per-join.

## Verdict

```
M-01: PROVEN
```

Proven, with the mechanism corrected and narrowed. The three certain facts are: (1) nothing on the send path gates on DAVE readiness — melodix asks once at join and skips even that on the cached-connection path, disgo never asks at all; (2) dave-go's `Encrypt` emits the frame unmodified rather than erroring when no ratchet is selectable, exactly as its interface documents, placing the obligation on the caller; (3) the fork implemented that obligation per frame and the migration deleted it along with its tests.

What the previous audit got wrong is the trigger. The ordinary member-join/leave re-key runs through `activatePendingEpochLocked`, which swaps the ratchet atomically under the same mutex `Encrypt` takes — no window exists there. The exposed states are `prepare_epoch(1)`, `select_protocol_ack`, and a degraded activation, each of which gives a 10-second grace on the *previous* ratchet before falling through to passthrough.

That grace changes the severity profile but not the verdict. Within it, frames are encrypted under an epoch the group has left: undecryptable, silent, and exactly the "796 frames over twelve seconds" the author recorded in the 08:58 run. Past it, the E2EE layer is simply gone. Both states are ones `ShouldHoldFrames()` reports truthfully and nothing reads.

The severity stays P0 for two reasons. The silent-audio half is not rare — it is the documented, reproduced symptom of issue #11. And the plaintext half is a security property that the project had already established, tested and shipped, then lost in a migration that was validated by compilation and a happy-path run.

---

# 2. M-02 — Voice transport death wedges playback

## A. Provider lifecycle

Exhaustive grep of disgo v0.19.6 for `opusProvider` / `OpusFrameProvider`:

| Site | What it does |
|---|---|
| audio_sender.go:41 | declares `Close()` on the interface |
| audio_sender.go:99-102 | `send()` — nil check, then `ProvideOpusFrame()` |
| conn.go:151-157 | `SetOpusFrameProvider`: `audioSender.Close()`, build new sender, `Open()` |
| conn.go:178-180 | `HandleVoiceStateUpdate(nil channel)`: `audioSender.Close(); audioSender = nil` |

**`OpusFrameProvider.Close()` is never called anywhere in disgo v0.19.6.** `AudioSender.Close()` is `s.cancelFunc()` and nothing more (audio_sender.go:149-151). `connImpl.Close` (conn.go:286-292) closes the voice gateway, the UDP conn and calls `removeConnFunc` — it does not touch `audioSender`.

## B/C/F. Transport failure paths, followed to the end

I built a probe in the scratchpad linking **the real `voice.NewAudioSender`** with melodix's `frameProvider`/`Sink.Stream` shape and a stub `Conn`/`UDPConn`.

**Scenario 1 — `net.ErrClosed` (socket closed, gateway gone, conn removed):**

```
audio flowing: udp writes=5, source reads=5
transport killed (net.ErrClosed on every write)
PROVEN: Stream still blocked 3s after transport death
  udp writes=6  source reads=6
```

Exactly one further write happened: `handleErr` matched `net.ErrClosed`, called `s.Close()`, the sender goroutine exited. `Stream` stayed blocked on `select { <-provider.done, <-stop }` and would block forever. The source stopped being drained at read 6, so no EOF will ever arrive to wake it either.

**Scenario 2 — any other write error (M-02's second variant, which the previous audit inferred from logging alone):**

```
Stream returned: EOF-sentinel
  udp write attempts=51  frames pulled from source=50
PROVEN variant: every write failed, yet the whole track was consumed
and Stream reported a normal end
```

`handleErr` logs and returns without closing, so the sender keeps pulling at 50 Hz. The track "plays" to completion, the player advances to the next one, history is recorded, and not one frame reached the network. This variant is **worse than a wedge**, because it is completely invisible: the bot reports normal playback of an entire queue into a dead socket. Note this also covers the `fmt.Errorf("failed to encrypt packet: %w", err)` path at udp_conn.go:275 — a wrapped error, never `net.ErrClosed`, so an encryption failure logs once per 20 ms indefinitely.

`HandleVoiceStateUpdate(nil channel)` (bot kicked/moved) reaches the same end state by a different door: `audioSender.Close(); audioSender = nil`, provider never told.

## D. `ErrVoiceTransport`

One producer, [sink.go:111](internal/discord/voice/sink/sink.go:111) inside `frameProvider.Close()`. Two consumers, [player.go:503](pkg/music/player/player.go:503) and [player.go:579](pkg/music/player/player.go:579). Since disgo never invokes `Close()` on a provider (proven by grep and by the probe, in which the instrumented `closeCalled` flag never fired), **both consumers are unreachable on the disgo backend**. The speaker backend produces it nowhere either. The entire transport-recovery subsystem — `maxVoiceTransportAttempts`, `RecoveryHard`/`RecoverySoft`, `PLAYER_TRANSPORT_RECOVERY_MODE`, `PLAYER_TRANSPORT_SOFT_ATTEMPTS` — is dead code.

## E. Deleted tests — correction to the previous audit

The previous audit said the two tests should be "restored". They cannot be. Reading them at `5953df1^`:

```go
func TestSendOpusTimeoutIsVoiceTransport(t *testing.T) {
	vc := &discordgo.VoiceConnection{OpusSend: make(chan []byte)}
	err := sendOpus(zerolog.Nop(), vc, []byte{1}, make(chan struct{}), 50*time.Millisecond)
	if !errors.Is(err, stream.ErrVoiceTransport) { ... }
}
```

They tested `sendOpus`, a **push-model** function that owned the write and could time out on it. The pull model has no equivalent function and no equivalent detection point. What disappeared is not the tests but the *capability*: under discordgo the sink drove the write and could observe its failure; under disgo the sender drives, and the only failure signal disgo offers — `OpusFrameProvider.Close()` — is never invoked. A new test against the pull model is needed; my probe is its exact shape.

## Verdict

```
M-02: PROVEN
```

**Minimal failure trace:**

```
1. /play starts track T in guild G. runPlayback -> Sink.Stream ->
   conn.SetOpusFrameProvider(fp); blocks on select{fp.done, stopCh}.
2. Voice websocket drops (lossy link) or the bot is dragged out of the channel.
   disgo: gatewayImpl.listen -> closeHandlerFunc -> connImpl.Close
          -> udp.Close(); manager.RemoveConn(G)
3. Next 20ms tick: defaultAudioSender.send -> conn.UDP().Write -> net.ErrClosed
   -> handleErr -> s.Close() -> cancelFunc -> sender goroutine exits.
4. ProvideOpusFrame is never called again. fp.done is never written.
   fp.Close() is never called by anyone.
5. Sink.Stream blocks forever. runPlayback never returns. doneCh never closes.
   Status message stays on "Now Playing". Queue never advances.
   OnPlaybackFailed never fires. The WS watchdog sees a healthy gateway.
   restart-voice and restart-session both leave the wedge in place.
6. Only /stop or /next (which close stopCh) unblock it.
```

---

# 3. M-08 — RecoveryStream race

## Reachability first

`ReopenAfterTransportFailure` has exactly one production caller: [player.go:591](pkg/music/player/player.go:591), strictly inside `if errors.Is(err, stream.ErrVoiceTransport)`. That error's sole producer is proven unreachable (M-02). **The recovery path cannot execute in the current code.** M-02 masks M-08 — verified, not assumed.

## If recovery is made reachable

I drove exactly the production interleaving against the **real `RecoveryStream`** (scratchpad module with `replace` to the repo; no repo files touched): `Packets()` starts the `BufferedReader` producer, a consumer drains it, and the test goroutine calls `ReopenAfterTransportFailure` — which is what `runPlayback` would do.

Result: **27 distinct `WARNING: DATA RACE` reports**, including a concurrent map access:

```
Write at 0x00c0001ba9c0 by goroutine 10:
  runtime.mapassign_faststr()
  stream.(*RecoveryStream).ReadPacket()  recovery.go:234   <- rs.retries[rs.curParser]++
  stream.packetView.ReadPacket()         recovery.go:308
  opus.(*BufferedReader).run()           buffer.go:44      <- read-ahead goroutine

Previous read at 0x00c0001ba9c0 by goroutine 9:
  runtime.mapaccess1_faststr()
  stream.(*RecoveryStream).Open()        recovery.go:121   <- rs.retries[parser] >= max
  stream.(*RecoveryStream).ReopenAfterTransportFailure()  recovery.go:373  <- player goroutine
```

In a non-race build this is `fatal error: concurrent map read and map write` — an unrecoverable runtime throw, not a recoverable error. (200 rounds without `-race` produced no fatal; the detector is the reliable oracle here, and the window is small but real.)

## Field table

| Field | Writers | Readers | Synchronization | Raced (observed) |
|---|---|---|---|---|
| `reader`, `cleanup` | `setActive` 401, `closeCurrent` 388 | `ReadPacket` 194 | **`rs.mu`** | no — correct |
| `closed` | `Close` 424 | `ReadPacket` 262 | `atomic.Bool` | no — correct |
| `retries` (map) | `Open` 130, `ReadPacket` 234, `reopen` 349 | `Open` 121, `shouldRecover` 325 | **none** | **yes — map read/write** |
| `parserIndex` | `Open` 134, `ReadPacket` 237 | `Open` 119 | **none** | **yes** |
| `seekSec` | `Open` 136, `ReadPacket` 207 | `Open`, `reopen`, `confirmOpen` 288, `shouldRecover` | **none** | **yes** |
| `curParser` | `Open` 137 | `ReadPacket` 234, `confirmOpen` 288 | **none** | **yes** |
| `firstRead` | `Open` 140, `ReadPacket` 204 | `ReadPacket` 203, 218 | **none** | **yes** |
| `fromCache` | `Open` 139, 108 | `ReadPacket` 222, 264, `confirmOpen` 285 | **none** | **yes** |
| `cacheDisabled` | `Open` 104, `ReadPacket` 225 | `Open` 100 | **none** | yes (same class) |
| `cacheWriter` | `startCacheWrite`, `commitCache`, `abortCache` | `ReadPacket` 210 | **none** | yes (same class) |
| `track.CurrentParser` | `Open` 138 | **`reply.trackChips` via `Player.CurrentTrack()`** | **none** | **yes — see M-07** |
| `track.Passthrough` | `Open` 125 | same | **none** | **yes** |
| `track.Cached` | `Open` 126 | same | **none** | **yes** |
| `buffered` | `Packets` 415 | `Close` 421 | none (single-goroutine by convention) | no |

The field comment — *"Every other field belongs to the producer goroutine alone, and Close reaches them only after Wait proves it has exited"* — is true of `Close`, which does `Stop → closeCurrent → Wait`, and false of `ReopenAfterTransportFailure`, which does neither.

## Verdict

```
M-08: PROVEN
```

> **If M-02 is fixed, does M-08 become a newly reachable race?**
> **Yes — unconditionally.** Making transport death observable is precisely what routes execution into `player.go:591`, and the first thing that does is call `ReopenAfterTransportFailure` against a live read-ahead producer. The two must be fixed in the same change, M-08 first.

---

# 4. M-03 — Cached sink provider bound to a dead session

## Lifecycle

```
main (never re-entered)
 └── discord.NewBot ─── voice.Service          [process lifetime]
                          ├── players[guild]    -> created once, holds
                          │     └── sinkProvider   (player.go:201, WRITE-ONCE)
                          └── sinkProviders[guild] (service.go:190, WRITE-IF-ABSENT)

RunSession  (re-entered on every restart)
 └── session.New -> disgo.New -> bot.Client
       └── VoiceManager  = voice.NewManager(client.UpdateVoiceState, ...)   [per client]
 └── dave := sink.NewDaveRegistry()                                         [per session]
 └── clientConn{client, dave} -> NewSinkProvider -> sink.NewProvider(client.VoiceManager, dave, ...)
```

Confirmed from source:

1. **Owner of the VoiceManager**: the `bot.Client`. Built at `disgo/bot/config.go:284`, closing over **that client's** `UpdateVoiceState`.
2. **Gateway session closes** → `RunSession` defer → `session.Close(ctx)` → `Client.Close` → `VoiceManager.Close(ctx)` (closes all conns, empties `m.conns`), then `Gateway.Close`.
3. **VoiceManager**: orphaned, but still callable. `CreateConn` still works; `voiceStateUpdateFunc` is the dead client's.
4. **DaveRegistry**: orphaned with it.
5. **Cached Provider**: survives. Only `StopAllPlayers` clears `sinkProviders` (service.go:380), and `stopAllPlayers` has exactly one call site — [session_run.go:139](internal/discord/session_run.go:139), inside the `<-ctx.Done()` (process shutdown) branch. The `<-disconnected` (unhealthy restart) branch does not call it.
6. **Cached Player**: survives, and `Player.sinkProvider` is written **only** at construction (player.go:201) and never reassigned — so even repopulating `sinkProviders` would not help an existing player.
7. **On the next `/play`**: `GetOrCreatePlayer` returns the cached `*Player` before touching `sinkProviders` at all. `runPlayback` calls the **stale** `Provider`.
8. **Can it work?** No — and it fails faster than the previous audit guessed. `conn.Open` → `voiceStateUpdateFunc` → `client.UpdateVoiceState` → `shard.Send` → `gatewayImpl.Send`:

```go
if g.status != StatusReady {
    return discord.ErrShardNotReady        // gateway.go:354-356
}
```

An immediate error, not a 15 s timeout.

## Verdict

```
M-03: PROVEN
```

Stronger than stated, and with a different signature. **Concrete session-restart sequence:**

```
1. Guild G plays normally on session N. Player_G and Provider_G created,
   Provider_G.manager = session N's VoiceManager.
2. WS_SILENCE_TIMEOUT (2m) expires -> notifyUnhealthy -> InvalidateAllSinks
   (Provider_G.conn = nil; manager NOT replaced) -> close(disconnected).
3. RunSession returns ErrSessionUnhealthy. Players are NOT stopped.
   main restarts after ~0-200ms. Session N+1: new Client, new VoiceManager,
   new DaveRegistry.
4. /play in guild G. GetOrCreatePlayer returns the cached Player_G,
   which holds Provider_G, which holds session N's manager.
5. runPlayback -> Sink() -> CreateConn(stale manager) -> conn.Open
   -> UpdateVoiceState on session N's closed gateway -> ErrShardNotReady.
6. Three attempts, 400ms + 800ms backoff -> ErrSinkUnavailable
   -> markPlaybackFailed -> "Playback failed" embed.
   startTrack's completion goroutine returns early on ErrSinkUnavailable,
   so the queue is not advanced and nothing retries.
7. PERMANENT for guild G for the process lifetime. No path rebuilds
   Player_G.sinkProvider.
```

This directly contradicts [docs/architecture.md](docs/architecture.md): *"since the voice service outlives individual sessions, queues and players survive reconnects — sinks just get invalidated and re-acquired."* The invalidation drops the connection; the thing bound to the session is the provider, and nothing drops that.

---

# 5. M-04 — Player.Stop state clobber

## The formal interleaving

The claim requires `Stop`'s phase 2 to land **after** a newer run's `startTrack` phase 2. Tracing lock acquisitions:

```
Stop phase 1  [p.mu]   doneCh := p.playbackDone; stopOnce.Do(close(p.stopPlayback)); target := p.target
              [—]      if p.IsPlaying() && doneCh != nil { <-doneCh | 10s }
Stop phase 2  [p.mu]   playing=false; starting=false; currTrack=nil
                       if disconnect { queue=nil; target=""; ReleaseSink(target) }
                       stopPlayback=new; playbackDone=new; stopOnce=Once{}

startTrack ph1 [p.mu]  stopPlayback=S_N; playbackDone=D_N; stopOnce=Once{};
                       starting=true; playing=false; currTrack=track
               [—]     rs.Open(0)                      <-- NETWORK I/O
startTrack ph2 [p.mu]  starting=false; playing=true; currTrack=track
                       stopCh := p.stopPlayback        <-- READ FROM THE FIELD
                       doneCh := p.playbackDone        <-- READ FROM THE FIELD
                       go runPlayback(track, rs, stopCh, doneCh)
```

**There is an existing identity mechanism, and it is not `clearIfCurrent`.** `startTrack` phase 2 reads `stopCh`/`doneCh` **from the live fields, after the open** (player.go:487-488). So if `Stop` phase 2 interleaves *between* startTrack's two phases, the run simply adopts the channels `Stop` just minted, and a later `Stop` can still signal it. That interleaving is self-healing.

Only the strict order `Stop.ph1 → startTrack.ph1 → startTrack.ph2 → Stop.ph2` produces the claimed orphan. For the completion goroutine to reach `startTrack.ph1`, `runPlayback` must have returned, which closes `doneCh` in its defer — waking `Stop` at the same instant. `Stop` then needs **one mutex acquisition**; the completion goroutine needs error checks, `p.mu`, `PlayNext`, `playNextMu`, `p.mu`, a network `rs.Open`, and `p.mu` again. The race is structurally lopsided in `Stop`'s favour.

`clearIfCurrent` does not help — it guards `runPlayback`'s clearing, not `Stop`'s. The saving grace is the late field read, not an identity check.

## Empirical attempt

I ran the real `Player` with the two goroutines that actually exist in production (the completion chain auto-advancing, and one command goroutine doing `Stop(false)` + `PlayNext` like `/next`), with jittered sleeps, checking after each `Stop(true)` whether any run was still live 300 ms later:

```
rounds=47  orphaned-after-Stop(true)=0  two-concurrent-runs=0
transient-anomalies=13  live-at-end=0
```

The 13 transients are the legitimate teardown window (`Stop` clears state, then the run's `Stream` notices the closed `stopCh` and exits). Zero orphans, zero concurrent runs, under `-race` and without.

I also checked the one branch that could delay phase 2 for 10 seconds — `runPlayback` blocked inside `p.sinkProvider.Sink(target)`, which does not select on `stopCh`. Even there the run adopts an already-closed `stopCh` when `Sink()` returns and aborts immediately. Self-healing again. That interleaving is real, but its consequence is M-09, not M-04.

## Verdict

```
M-04: UNPROVEN
```

The defect is real as a *shape*: `Stop`'s phase-2 reset has no identity guard, and `PlayNext` calls `Stop` outside `playNextMu` ([player.go:293](pkg/music/player/player.go:293)). But the specific corruption the previous audit described did not occur in 47 aggressive rounds, and the late `stopCh` read explains structurally why it is hard to reach. **I am downgrading this from P1 to a latent P3 hardening item and correcting the previous audit.** One dependency worth carrying forward: enabling async dispatch (M-05) would let two command goroutines call `Stop`/`PlayNext` concurrently, which widens this window — so the guard should be added *before* that change, not because of a bug observed today.

---

# 6. M-05 — Synchronous event dispatch

Answering the ten questions against the pinned source:

1. **Synchronous by default?** Yes. `defaultEventManagerConfig()` (bot/event_manager_config.go:9-13) leaves `AsyncEventsEnabled` false.
2. **`WithAsyncEventsEnabled` absent?** Yes. Grep across `internal/` and `cmd/` for `WithEventManagerConfigOpts` / `WithAsyncEventsEnabled` returns nothing; the only `disgo.New` is [session.go:72](internal/discord/session/session.go:72) and it passes neither.
3. **Which goroutine calls `DispatchEvent`?** The single `go g.listen(t, ...)` goroutine (gateway.go:287), via `g.eventHandlerFunc` → `eventManagerImpl.HandleGatewayEvent`.
4. **Inline?** Yes — `listener.OnEvent(event)` with no `go` when async is off (event_manager.go:158).
5. **Listener mutex held?** Yes, `e.eventListenerMu` for the whole loop, and `HandleGatewayEvent` additionally holds `e.mu` for the whole handling.
6. **One gateway read goroutine?** Yes; no `ShardManager` is configured, so one shard, one `listen`.
7. **Command work inline?** Yes. `onApplicationCommand` → `runGuardedInteraction` → `c.Run(cmdCtx, inv)`, all inside the listener.
8. **Network I/O?** Yes: `slashCtx.Defer()` (REST), `c.Bot.ResolveTracks` (InnerTube/SoundCloud HTTP, playlist expansion with paged continuations), `rs.Open` (HTTP or spawning ffmpeg), `WithCommandLogger`'s REST fallbacks.
9. **Sleeps?** Yes — `onReady` → `syncer.SyncGuildCommands` per guild, with a deliberate `time.Sleep(25 * time.Millisecond)` after every write ([syncer.go:107,121,134](internal/discord/cmdsync/syncer.go:107)).
10. **Concurrent events?** No.

**Empirical confirmation** with melodix's exact configuration (real `bot.NewEventManager`, no async option), dispatching two events from two goroutines:

```
max concurrent listener executions = 1
wall clock for 2 x 60ms listeners = 121.2ms
PROVEN: DispatchEvent serialises listeners under eventListenerMu
```

## `COMMAND_PARALLELISM`

Implemented at [session_run.go:105](internal/discord/session_run.go:105) → `execguard.New(timeout, 16)` → a buffered channel of 16 acquired in `runWithCommandContext`. Since every caller (`onApplicationCommand`, `onComponentInteraction`, `onMessageCreate`) is a listener, and listeners are serialised, **the semaphore can never hold more than one token and can never block**. The knob and the package are inert.

## Verdict

```
M-05: PROVEN
```

**Architectural consequence, quantified:**

- **Throughput**: the bot's command concurrency is exactly 1, permanently, regardless of `COMMAND_PARALLELISM`. Its documented default of 16 is a claim about a property the runtime does not have.
- **Interaction deadline**: Discord requires acknowledgement within 3 s *of interaction creation*. A `/play` resolving a 100-item playlist on a throttled link holds the read loop for seconds; any interaction arriving in that window is dispatched late, `Defer()` returns 10062, and the user sees "The application did not respond". This is the most likely user-visible symptom and it scales with link quality.
- **Startup**: `onReady` runs `SyncGuildCommands` for every guild inline — one REST GET plus up to N writes plus N × 25 ms sleeps, per guild, with the socket unread throughout.
- **Watchdog self-starvation**: `events.Raw` (`tracker.MarkWSNow`) and `events.HeartbeatAck` (`Session.onHeartbeatAck`) are dispatched through the same serialised path. Any command longer than `WS_SILENCE_TIMEOUT` (default 2 m) would make the bot declare its own healthy gateway dead. That margin is generous, so this is a structural hazard rather than a live one — but it is the same class of failure the migration was undertaken to eliminate, reached through a different door.
- **Voice interaction**: `Provider.Sink` runs on the `runPlayback` goroutine, not the listener, so joins are *not* deadlocked. That part of the design is correct and I verified it.

The migration reproduced discordgo's handler code without reproducing its dispatch model: discordgo runs each handler in its own goroutine unless `SyncEvents` is set; disgo is the opposite default.

---

# 7. M-06 — Mirrored voice connection state

Connection removal paths, and whether melodix is told:

| Path | Removes from manager? | Melodix notified? |
|---|---|---|
| `Provider.releaseLocked` → `conn.Close` → `removeConnFunc` | yes | yes — it initiated it |
| `Provider.Sink` open-failure → `removeConn` | yes | yes |
| `connImpl.handleGatewayClose` (voice WS close, non-reconnectable code, or failed auto-reconnect) → `c.Close` → `removeConnFunc` | **yes** | **no** |
| `Client.Close` → `VoiceManager.Close` (session restart) | yes, empties the map | **no** |
| `connImpl.HandleVoiceStateUpdate(ChannelID == nil)` | no (closes gw/udp only) | **no** |

The close propagation is confirmed at `voice/gateway.go:515-516` (`go g.closeHandlerFunc(g, err, reconnect)`) and `voice/gateway.go:358-359` (reconnect failure) → `connImpl.handleGatewayClose` (conn.go:269-273) → `c.Close(ctx)` → `c.removeConnFunc()`.

`Provider` never consults `manager.GetConn(guildID)`; its entire liveness test is its own two fields:

```go
if p.conn != nil && p.currentChannelID == target {
    return &Sink{conn: p.conn, log: p.log}, nil
}
```

> **Can `Provider.conn != VoiceManager.GetConn(guildID)`?** **Yes.**

```
1. Track playing in guild G, Provider_G.conn = C, currentChannelID = "123".
2. Voice websocket closes with a non-reconnectable code (4006 session no
   longer valid, 4014 disconnected), or AutoReconnect exhausts.
3. disgo: listen -> closeHandlerFunc -> connImpl.Close
     -> voiceStateUpdateFunc(nil), gateway.Close(), udp.Close(),
        removeConnFunc() -> manager.RemoveConn(G)
   => manager.GetConn(G) == nil, Provider_G.conn == C   (DIVERGED)
4. Track ends; next track: Sink("123") takes the fast path and returns a
   Sink over the dead C.
5. SetOpusFrameProvider on C -> new sender -> UDP write -> net.ErrClosed
   -> M-02 wedge.
```

**DAVE registry cleanup** — the previous audit overstated this. `Forget` is indeed skipped on paths 3–5, so the session stays in `DaveRegistry.sessions[guildID]` un-`Close()`d. But dave-go documents the bound itself:

> *"Forgetting to call Close is not a permanent leak — the recovery watchdog re-arms at most maxRecoveryAttempts times, waiting recoveryTimeout per attempt … then exits on its own"*

With melodix's `WithRecoveryTimeout(5s)`, that is ~15 s of log pollution and re-armed invalidations on a channel the bot has left, then the goroutines exit. A later join creates a new Conn → a new session → `r.put` overwrites the map slot, so the stale session is never *consulted*. **Corrected: the DAVE sub-claim is a bounded nuisance, not a leak.** The real harm is the stale `Provider.conn`.

## Verdict

```
M-06: PROVEN
```

---

# 8. M-07 — Shared mutable Track

`parsers.Track` is passed as `*parsers.Track` into `RecoveryStream`, which writes three of its fields inside `Open`:

```go
rs.track.Passthrough = false   // recovery.go:125
rs.track.Cached      = false   // recovery.go:126
rs.track.CurrentParser = parser // recovery.go:138
```

`Open` is reachable from `ReadPacket` (recovery.go:238, the `immediate_failure_switching_parser` branch), which under `BUFFER_AHEAD_MS > 0` — **default 30000** — runs on the `BufferedReader` read-ahead goroutine. The same pointer is returned by `Player.CurrentTrack()` (`p.mu` protects the pointer, not the pointee) and read by `reply.trackChips` via `voice.Service.watchPlayerStatus`.

`cloneTrack` exists and is applied to the recorder and `failedSnapshot` — the aliasing was seen, and the UI path was missed.

**Empirical proof** against the real `Player` and real `RecoveryStream`, with a reader that does exactly what `watchPlayerStatus` → `NowPlayingEmbed` → `trackChips` does:

```
WARNING: DATA RACE
Write at 0x00c0001c19e9 by goroutine 12:
  stream.(*RecoveryStream).Open()        recovery.go:126   <- rs.track.Cached = false
  stream.(*RecoveryStream).ReadPacket()  recovery.go:238
  stream.packetView.ReadPacket()         recovery.go:308
  opus.(*BufferedReader).run()           buffer.go:44      <- read-ahead goroutine
   (created at player.go:548 -> runPlayback -> rs.Packets())

Previous read at 0x00c0001c19e9 by goroutine 9:
  m07_test.go:128                                          <- _ = tr.Cached after CurrentTrack()
```

11 races across `track.Cached`, `track.Passthrough` and `track.CurrentParser`, from both the status-watcher goroutine and a direct `CurrentTrack()` reader.

## Verdict

```
M-07: PROVEN
```

**And it should be ranked above M-08.** M-08 requires the currently-unreachable transport-recovery path. M-07 fires on the ordinary **mid-track parser switch** — a normal, expected, logged event that the `now_playing_parser_corrected` logic exists to handle. It is reachable in production today, on the default configuration.

---

# 9. M-09 — ReleaseSink under player.mu

`Player.Stop` phase 2 holds `p.mu` across `p.sinkProvider.ReleaseSink(target)` ([player.go:377](pkg/music/player/player.go:377)).

Nested call trace, not a sum of constants:

```
Stop.phase2 [holds p.mu]
 └─ Provider.ReleaseSink
     └─ p.mu.Lock()                        <-- Provider's mutex
         contends with Provider.Sink, which holds it via `defer` across:
           conn.Open(joinCtx)              voiceJoinTimeout   = 15s
           awaitEncryption:
             time.Sleep(voiceReadyDelay)   VOICE_READY_DELAY_MS = 500ms
             dave.WaitReady(ctx)           daveReadyTimeout   = 10s
     └─ releaseLocked -> conn.Close(ctx)   voiceCloseTimeout  = 10s
           connImpl.Close: voiceStateUpdateFunc (ctx-bounded),
                           gateway.Close(), udp.Close()  (local)
     └─ removeConn -> manager.RemoveConn + dave.Forget -> Session.Close()  (non-blocking)
```

**Is the overlap actually reachable?** Yes, and I verified the enabling condition. `startTrack` sets `playing = true` *before* spawning the goroutine (player.go:484-497), so `IsPlaying()` is true throughout `Sink()`. `runPlayback` does not select on `stopCh` while inside `Sink()`. So:

```
runPlayback enters Sink(); conn.Open is waiting on a join that will not
complete (missing Connect permission => no VoiceStateUpdate ever arrives).
/stop -> Stop(true) phase 1: IsPlaying()==true, waits on doneCh
      -> doneCh does not close (runPlayback is in Sink())
      -> 10s timeout fires
      -> phase 2 takes p.mu, calls ReleaseSink, which blocks on Provider.mu
         for the remaining ~5.5s of the 15s join, plus conn.Close
```

**Maximum blocking duration**: ~15 s of `p.mu` held in the realistic case (25.5 s if `Stop` is entered at the very start of a `Sink()` that then waits out both the join and the DAVE handshake).

**Who blocks behind `p.mu`**: `IsPlaying`, `CurrentTrack`, `Queue`, `EnqueueTrackInfos`, `ChannelID`, `SetGuildID`, `SetRecorder`, `clearIfCurrent`, `onParserConfirmed`, and every `emitStatus` caller — i.e. every command path for that guild plus the read-ahead goroutine's confirmation callback.

**Does it combine with M-05?** Yes, and this is the compounding that matters. Under serial dispatch, the blocked caller is the single gateway listener goroutine, so a stall in one guild's `p.mu` stalls **the entire bot** — every guild, every command, and the watchdog's own event intake — for up to 15 seconds.

## Verdict

```
M-09: PROVEN
```

---

# 10. Cross-finding dependency graph

```
                    ┌─────────────────────────────────────────┐
                    │  M-02  transport death never surfaces   │
                    └───────┬─────────────────────┬───────────┘
                     MASKS  │                     │  ABSORBS
                            ▼                     ▼
                    ┌───────────────┐    ┌──────────────────┐
                    │ M-08  Recovery│    │ M-03 stale prov. │
                    │ Stream race   │    │ M-06 stale conn  │
                    │ (27 races +   │    │  both converge on│
                    │  map r/w)     │    │  a dead sink     │
                    └───────────────┘    └──────────────────┘

  M-05 serial dispatch ──AMPLIFIES──► M-09  (guild-local stall becomes bot-wide)
  M-05 serial dispatch ──SUPPRESSES─► M-04  (only one command goroutine exists)
  M-01 ◄──DEPENDS ON── DAVE session lifecycle: awaitEncryption is per-join,
                       and Sink()'s cached-conn fast path skips even that
  M-07 ◄──SHARES ROOT──► M-08  (RecoveryStream.Open writes unsynchronized
                       fields from the read-ahead goroutine) but M-07 is
                       reachable TODAY and M-08 is not
```

Verified edges, not asserted ones:

- **M-02 masks M-08** — *verified*. `ReopenAfterTransportFailure` has one production caller, [player.go:591](pkg/music/player/player.go:591), strictly inside the `ErrVoiceTransport` branch; that error has one producer, [sink.go:111](internal/discord/voice/sink/sink.go:111), which disgo never invokes. The recovery path is unreachable, therefore the race cannot fire. **Fixing M-02 activates M-08 unconditionally.**
- **M-02 absorbs M-03 and M-06** — *verified*. Both of those end in "the provider hands out a sink over a connection that cannot carry audio." Under M-03 the join fails fast (`ErrShardNotReady`) and at least produces a visible "Playback failed". Under M-06 the join is *skipped* (the fast path returns the stale conn), so the failure lands in the M-02 wedge instead: silent, permanent, no error anywhere.
- **M-05 amplifies M-09** — *verified*. Serial dispatch means the blocked caller is the gateway read goroutine.
- **M-05 suppresses M-04** — *verified*. Only the completion goroutine and one command goroutine can contend today. Enabling async dispatch removes that limit, so M-04's guard should land *with* the M-05 fix.
- **M-01 and the DAVE lifecycle** — *verified*. `awaitEncryption` is the only gate, and `Provider.Sink`'s `p.conn != nil && p.currentChannelID == target` fast path returns before reaching it, so tracks 2..N of a queue are ungated even at join granularity.
- **New edge not in the previous audit**: M-07 and M-08 are the same root defect (`RecoveryStream.Open` mutating unsynchronized state from the read-ahead goroutine) seen from two sides. One fix — extending `rs.mu` to cover the fields `Open` writes — addresses the engine half of both.

---

# 11. Minimal reproduction matrix

| Finding | Trigger | Observable symptom | Why current code fails | Minimal test target |
|---|---|---|---|---|
| **M-01** | E2EE channel; gateway sends `prepare_epoch(1)` or `select_protocol_ack(pv>0)`, then >10 s without a Welcome | 10 s of silence for all listeners, then frames with no E2EE layer; `dave.State().Stats.PassthroughFrames` climbing | Nothing on the send path calls `Ready()`/`ShouldHoldFrames()`; `awaitEncryption` ran once, at join | A fake `godave.Session` whose `Ready()`/`ShouldHoldFrames()` can be flipped, driving melodix's `frameProvider`; assert no frame is produced while hold is true |
| **M-02** | Voice UDP/WS dies mid-track (kick, move, link drop) | "Now Playing" forever; queue frozen; no error; only `/stop` or `/next` recovers | `OpusFrameProvider.Close()` is never called by disgo; `Sink.Stream` has no third wake-up path | Real `voice.NewAudioSender` + stub `Conn`/`UDPConn` whose `Write` returns `net.ErrClosed`; assert `Sink.Stream` returns `ErrVoiceTransport` within N ms *(my scratchpad probe is exactly this)* |
| **M-02b** | Any UDP write error other than `net.ErrClosed`/`ErrGatewayNotConnected` | Whole queue "plays" with zero audio delivered; history recorded normally | `handleErr` logs and continues; the reader keeps draining | Same harness, `Write` returns a generic error; assert the track does not complete successfully |
| **M-03** | One gateway session restart (2 min WS silence), then `/play` in a guild that already had a player | "Playback failed" on every `/play` in that guild, forever | `Player.sinkProvider` write-once; `sinkProviders` cleared only in `StopAllPlayers`, which the restart path never calls | A `SinkProviderFactory` returning a generation-tagged fake; bump the generation, assert the next `Sink()` uses the new one |
| **M-05** | Two interactions within one slow command | Second interaction answers late → 10062 → "The application did not respond" | `disgo.New` omits `WithAsyncEventsEnabled`; listeners run inline under `eventListenerMu` | Real `bot.NewEventManager` with melodix's options; assert max concurrent listener executions > 1 *(my probe asserts the inverse today)* |
| **M-06** | Voice gateway closes with a non-reconnectable code mid-queue | Next track produces silence and then the M-02 wedge | `Provider` never compares `p.conn` against `manager.GetConn(guildID)` | Stub `voice.Manager` whose `RemoveConn` can be called externally; assert `Sink()` rejoins rather than returning the stale conn |
| **M-07** | Any mid-track parser switch (`immediate_failure_switching_parser`) while the status watcher renders | `-race` failure; in production, a torn read of `CurrentParser`/`Cached` in the Now Playing chip | `Player.CurrentTrack()` returns the live pointer; `RecoveryStream.Open` writes its fields from the read-ahead goroutine | Real `Player` + a parser chain whose first entry dies on first read + a goroutine calling `CurrentTrack()` and reading the chip fields, under `-race` *(my probe)* |
| **M-08** | Only after M-02 is fixed: `ErrVoiceTransport` → `ReopenAfterTransportFailure` with `BUFFER_AHEAD_MS > 0` | `fatal error: concurrent map read and map write` → process death; or silently corrupted `parserIndex`/`seekSec` | `rs.mu` covers only `reader`/`cleanup`; `Open` mutates ten more fields from a second goroutine | Real `RecoveryStream` + `Packets()` + a consumer + concurrent `ReopenAfterTransportFailure`, under `-race` *(my probe)* |
| **M-09** | `/stop` while `runPlayback` is inside a `Sink()` that cannot complete (missing Connect permission) | Every command in every guild hangs ~15 s | `ReleaseSink` called under `p.mu`; `Provider.mu` held across join + DAVE wait | Fake `Provider` whose `Sink()` blocks; assert `Queue()`/`IsPlaying()` stay responsive during `Stop(true)` |

---

# 12. Final classification

## P0

* **M-01** — **PROVEN** *(mechanism corrected: the ordinary member-change re-key is atomic and safe; exposure is `prepare_epoch(1)` / `select_protocol_ack` / degraded activation, after dave-go's 10 s retained-ratchet grace expires)*
* **M-02** — **PROVEN** *(empirically, both variants)*

## P1

* **M-03** — **PROVEN** *(stronger than claimed: fails fast with `ErrShardNotReady`, and is permanent per guild)*
* **M-04** — **UNPROVEN** *(mechanism real; corruption not reproducible in 47 rounds; downgrade to P3)*
* **M-05** — **PROVEN** *(empirically)*
* **M-06** — **PROVEN** *(DAVE sub-claim weakened: dave-go self-bounds the leak to ~15 s)*
* **M-07** — **PROVEN** *(promote above M-08: reachable today on default config)*
* **M-08** — **PROVEN** *(27 races incl. concurrent map read/write; currently masked by M-02)*
* **M-09** — **PROVEN** *(~15 s of `player.mu`, bot-wide under M-05)*

## Most important corrections to the previous audit

1. **M-04 drops from P1/asserted to UNPROVEN/P3.** `startTrack` reads `stopCh`/`doneCh` from the live fields *after* the open (player.go:487-488), which makes the common interleaving self-healing. 47 aggressive rounds produced zero orphaned or concurrent runs.
2. **M-01's trigger was wrong.** "Every re-key" is false: `activatePendingEpochLocked` swaps the ratchet atomically under the mutex `Encrypt` takes. The real windows are narrower and gated by a 10-second retained-ratchet grace the previous audit did not know about. The verdict stands; the reasoning had to be rebuilt.
3. **M-07 should outrank M-08.** M-08 is unreachable today; M-07 fires on the ordinary parser-switch path with default settings.
4. **M-03's failure signature is different and worse.** Not 15-second join timeouts — an immediate `ErrShardNotReady`, and permanent for the guild because `Player.sinkProvider` is write-once, not merely because the map is memoized.
5. **M-06's DAVE-session leak is bounded, not indefinite.** dave-go's own docs cap it at `maxRecoveryAttempts × recoveryTimeout` ≈ 15 s with melodix's setting.
6. **M-02's deleted tests cannot be "restored".** They tested `sendOpus`, a push-model function that no longer exists. What was lost is the detection capability, not the test file.
7. **M-02 has a second, quieter variant** the previous audit only inferred: a generic UDP write error drains the entire track at full speed into a dead socket while reporting a clean end. Now demonstrated.

## Proven architectural facts

Established from source and executable evidence, not inference:

- `disgo v0.19.6` never calls `OpusFrameProvider.Close()`. `AudioSender.Close()` is `cancelFunc()` alone; `connImpl.Close` does not touch the sender.
- `disgo v0.19.6`'s `voice` package contains zero calls to `godave.Session.Ready()`; the interface documents that gating is the caller's job.
- `dave-go v0.5.1` `Encrypt` returns the frame unmodified (`session.go:354`) when `selectSendRatchetLocked()` is nil, and counts it in `stats.PassthroughFrames`.
- `dave-go`'s `sendRetentionTTL` is `epochRetention = 10 * time.Second`, and `activatePendingEpochLocked` is atomic under `s.mu`.
- disgo's default event dispatch is serial; melodix does not enable async; therefore command concurrency is exactly 1 and `COMMAND_PARALLELISM` is inert.
- `voice.NewManager` is built per `bot.Client` and closes over that client's `UpdateVoiceState`; a stale manager's join fails immediately with `discord.ErrShardNotReady`.
- `Player.sinkProvider` is written once, at construction, and never reassigned.
- `RecoveryStream.Open` mutates `retries`, `parserIndex`, `seekSec`, `curParser`, `firstRead`, `fromCache`, `cacheDisabled`, `cacheWriter` and three `*parsers.Track` fields outside `rs.mu`, from whichever goroutine calls it.
- `Provider.Sink` holds `Provider.mu` across `conn.Open` (15 s) and `awaitEncryption` (0.5 s + 10 s).
- The fork implemented a per-frame `holdFrames(dave) = dave != nil && !dave.Ready()` gate with four tests, all deleted in `5953df1`.

## Unknowns

- **How often `prepare_epoch(1)` / `select_protocol_ack` fires in practice**, and how often no Welcome arrives within 10 s. The author's 08:58 log (796 frames / 12 s) is one datum. Needs production instrumentation of `dave.State().Stats.PassthroughFrames` and `TransitionWindows`.
- **Whether a 4006/4014 voice close is common on this link.** M-06's trigger frequency is unmeasured.
- **Whether the guild's Discord application has the three privileged intents enabled** (`IntentsAll`). If not, the gateway is refused with 4014 and everything above is moot until it is fixed.
- **The two upstream disgo races** (`connImpl.audioSender` written from both the gateway goroutine and the player goroutine; `defaultAudioSender.cancelFunc` written in the sender goroutine and read by `Close()`). I did not exercise them; they need a live voice connection under `-race`.
- **M-04's true window width under async dispatch.** Not measurable until M-05 is changed.

## Recommended next step

Smallest ordered sequence. No refactor; every item is local and preserves the current architecture.

1. **Add a transport-death test harness** for the pull model — real `voice.NewAudioSender`, stub `Conn`/`UDPConn`. It is the regression test for M-02 and M-02b and costs one file.
2. **Serialize `RecoveryStream` first** (M-08 + the engine half of M-07). Extend `rs.mu` to cover the fields `Open` writes and `ReadPacket` reads. This must land **before** step 3, because step 3 is what makes `ReopenAfterTransportFailure` reachable.
3. **Make transport death observable** (M-02, and M-06 falls out of it). In `Sink.Stream`, add a third `select` case: a ticker that compares `p.manager.GetConn(guildID)` against the conn the sink was built on and checks `conn.ChannelID() != nil`; on mismatch, `finish(ErrVoiceTransport)`. The same check is what reconciles `Provider.conn` with the manager.
4. **Gate the send path on DAVE readiness** (M-01). Carry the guild's `*davesession.Session` into `Sink` — `DaveRegistry` already holds it and `NewProvider` already receives it — and have `ProvideOpusFrame` return a zero-length frame while `dave.ShouldHoldFrames()`. That is the `holdFrames` call site restored, with the predicate already correctly translated. Port the four deleted `dave_hold_test.go` cases against it.
5. **Have `Player.CurrentTrack()` return `cloneTrack(*p.currTrack)`** (M-07, UI half). One line.
6. **Move `ReleaseSink` outside `p.mu`** (M-09): capture `target` and the provider under the lock, release, then call. One line reordered.
7. **Rebuild the sink provider per session** (M-03). Cheapest form that preserves the design: bump a generation counter in `RunSession`, store it alongside each cached provider, and rebuild when it differs — plus clear `sinkProviders` in the unhealthy-restart path, not only in `StopAllPlayers`. Note that step 7 is incomplete without addressing `Player.sinkProvider` being write-once; the smallest fix there is to have the player hold the `SinkProviderFactory` rather than the provider.
8. **Then, and only then, address dispatch** (M-05): run the command body on its own goroutine inside `runGuardedInteraction` (safer than global async, since it preserves ordering for `Ready`/`GuildJoin`). Add the `playNextMu` guard to `Player.Stop` in the same change, because this is what widens M-04.

Items 1–6 are independent of each other except for the 2-before-3 ordering. Item 8 should not be attempted before 2 and the M-04 guard.