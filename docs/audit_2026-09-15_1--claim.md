> **Historical record.** This is one of four documents from the audit of
> 15 September 2026, kept as written. Every file path, line number and code
> quotation in it describes the tree *at that date* — most have since moved
> or been fixed, and the links are relative to the repository root rather
> than to `docs/`. Do not read it as a description of the current code.
>
> What the tree looks like now: [architecture.md](architecture.md).
> The rules that came out of this, and what enforces each:
> [ownership.md](ownership.md).

I'll start by mapping the repository structure before reading anything in depth.

I read the repository, the disgo v0.19.6 source, godave v0.2.0 and dave-go v0.5.1 in the module cache, and the git history of the migration. `go vet ./...` is clean; `go test ./...` and `go test -race ./...` both pass. Below is the audit.

---

# Executive verdict

Melodix is a genuinely well-structured codebase with a clear engine/adapter split that is mechanically enforced, and the migration was executed with unusual discipline at the *type* level — the adapter boundary holds, `pkg/music` really is Discord-free, and the DAVE seam crossed over intact. But the migration was validated by compilation and by a happy-path live run, and three load-bearing *behaviours* did not survive it. Audio now leaves the bot in the clear during every MLS re-key, because disgo's send path never consults `godave.Session.Ready()` and melodix's only encryption gate fires once at join — which is precisely bug #11 that `docs/dave-todo.md` records as fixed. A voice connection that dies mid-track wedges playback forever, because `frameProvider.Close()` is never called by disgo, making `ErrVoiceTransport` unreachable and the player's entire transport-recovery subsystem dead code — and the two tests that pinned exactly this contract were deleted in the migration and not replaced. The voice service caches a sink provider bound to one gateway session, so the reconnect survival the architecture document advertises does not work. Beneath that, the bot now runs every command on disgo's single gateway read goroutine, which silently makes `COMMAND_PARALLELISM` a no-op and puts network I/O in front of the interaction ACK deadline. The concurrency model in `player` is carefully reasoned in most places and then has one unguarded hole (`Stop`) that can orphan a playback run. The naming is mostly good, with one real offender: two packages called `sink` exporting two different things called `Provider`. This is not an architectural zoo — there is a recognisable "Melodix way" — but the effort has been spent on the parts that were already safe, and the voice layer, which is the part that actually broke, has zero tests.

---

# Architecture map

**Two binaries, one engine.**

```
cmd/discord ──┐                          cmd/cli ──┐
              ▼                                    ▼
      internal/discord (Bot)                 pkg/music/player.Player
        │  session lifecycle                       │
        │  handlers  ──► cmdadapter (neutral) ◄── internal/command/*
        │  reply (disgo impls of cmdadapter)
        │  cmdsync / cmdlogger / execguard / watchdog / perm
        │
        └─► voice.Service  (outlives sessions)
              ├─ players[guildID]      *player.Player
              ├─ sinkProviders[guildID] musicsink.Provider   ← memoized forever
              └─ guildMusicStatus[guildID] (channel+message id)
                     │
                     ▼
             voice/sink.Provider (disgo voice.Manager + DaveRegistry)
                     │
                     ▼
      pkg/music: player → stream.RecoveryStream → parsers → opus.Reader
                                  │
                                  └─ cache (global), opus.BufferedReader (above recovery)
```

**Dependency direction** is clean and checked: `TestLibraryStaysDiscordFree` bans `disgo`/`discordgo` from `pkg/music`, `TestDiscordStaysBehindTheAdapter` bans them outside `internal/discord`. `internal/command/*` depends on `cmdadapter` types only. The `pkg/music → internal` direction never occurs.

**Runtime components.**
- `Bot` ([internal/discord/bot_state.go](internal/discord/bot_state.go)) — holds `storage`, `cfg`, `voice`, and three `atomic.Value` slots swapped per session (`sessionCtx`, `cmdGuard`, `conn`).
- `conn` ([internal/discord/conn.go](internal/discord/conn.go)) — a two-method interface (`API()`, `NewSinkProvider()`) published on session open, cleared on close. This is a good abstraction.
- `voice.Service` — per-guild players, sink providers, status messages. Deliberately outlives sessions.
- `session.Session` — a thin wrapper over `*bot.Client` recording heartbeat ACKs.

**Event flow.** disgo's single `gatewayImpl.listen` goroutine → `eventManagerImpl.HandleGatewayEvent` (holds `e.mu`) → `DispatchEvent` (holds `eventListenerMu`, **synchronous**, since `WithAsyncEventsEnabled` is not set) → melodix's six listeners in registration order.

**Command flow.** `onApplicationCommand` → `command.DefaultRegistry.Get(name)` → build `*cmdadapter.SlashInteractionContext` → `runGuardedInteraction` → `execguard` → middleware chain (`group → guildOnly → perms → audit log`) → `Adapter.Run` → `Handler.Run(ctx interface{})`.

**Playback flow.** `/play` → `Defer()` → `playback.Join` → `ResolveTracks` (network) → `EnqueueTrackInfos` → `PlayNext` → `playNextMu` → dequeue → `startTrack` → `RecoveryStream.Open` (network) → spawn `runPlayback` goroutine → `sinkProvider.Sink(target)` (voice join) → `Sink.Stream` → `conn.SetOpusFrameProvider` → disgo's 20 ms sender pulls `ProvideOpusFrame`.

**Queue flow.** `[]parsers.Track` under `p.mu`; `playNextMu` serialises dequeue+open; the completion chain is `runPlayback → completion goroutine → PlayNext → startTrack → new goroutine`.

**Persistence flow.** `keshon/datastore`, four collections, two guild indexes, retention trimming on append. Single-process directory lock.

**Concurrency model.** Three families of goroutine touch a player: the gateway listen goroutine (all commands), the per-run `runPlayback` goroutine, and the `opus.BufferedReader` read-ahead producer. Plus the per-guild `watchPlayerStatus` consumer, the WS-silence watchdog, and disgo's per-conn audio sender.

**Lifecycle/shutdown.** `main` runs `RunSession` in a restart loop; `RunSession` blocks on `ctx.Done()` or `disconnected`; teardown is bounded by `closeWithin` with per-phase timing.

---

# Findings

## P0

### M-01 — Audio is sent unencrypted during every DAVE re-key
**Severity** P0 · **Cost** very high (security property + the bug the project already fixed once) · **Confidence** high

**Location** `internal/discord/voice/sink/sink.go:frameProvider.ProvideOpusFrame`, `internal/discord/voice/sink/provider.go:awaitEncryption`, against `disgo/voice/udp_conn.go:Write`.

**Evidence.** The `godave.Session` contract is explicit:

> "AudioSenders should hold frames while Ready returns false instead of sending them unencrypted. Encrypt still forwards frames unmodified (passthrough) rather than erroring, so callers that don't gate on Ready continue to work."

disgo's `udpConnImpl.Write` calls `u.daveSession.Encrypt(...)` and never calls `Ready()`. Grepping `disgo/voice/*.go` for `.Ready()` returns nothing. dave-go's `Encrypt` (session.go:337) returns `copy(encryptedFrame, frameData)` — verbatim passthrough — whenever `selectSendRatchetLocked()` is nil, incrementing `stats.PassthroughFrames`.

Melodix's only gate is `Provider.awaitEncryption`, called **once per join**, and skipped entirely on the cached-connection path:

```go
if p.conn != nil && p.currentChannelID == target {
    return &Sink{conn: p.conn, log: p.log}, nil   // no awaitEncryption
}
```

**Why it is a problem.** An MLS group re-keys whenever a member joins or leaves. During that window `ShouldHoldFrames()` is true and `Ready()` is false, and melodix keeps pushing frames. They go out in the clear on a channel Discord has marked end-to-end encrypted, and every receiver expecting E2EE discards them.

This is not hypothetical: `docs/dave-todo.md` records it as links 3 and 4, fixed on the fork by "Holding frames rather than sending them unencrypted, in both of the ways an epoch dies." That hold lived in the fork's `voice.go` send loop, which was deleted. The document's claim that "the disgo migration did not touch any of this" is true of the `godave.SessionCreateFunc` seam and false of the hold.

**Failure mode.** Silent audio for every listener for the duration of each re-key, plus plaintext media on an E2EE channel — which the author's own document notes a server is "entitled to close the connection for it." The observed instance was 796 frames over 12 seconds.

**Direction.** Gate in `ProvideOpusFrame`, the one point melodix owns on the send path: hold (return a zero-length frame, or block briefly) while `dave.ShouldHoldFrames()`. That requires `Sink` to carry the guild's `*davesession.Session`, which `DaveRegistry` already has. Lowering `DaveRecoveryTimeout` to 5 s bounds the symptom; it does not close the hole. Also consider surfacing `dave.State().Stats.PassthroughFrames` in a log line — today the condition is completely invisible.

---

### M-02 — A mid-track voice failure wedges playback forever; the player's transport recovery is unreachable
**Severity** P0 · **Cost** very high · **Confidence** high

**Location** `internal/discord/voice/sink/sink.go:Sink.Stream`, `frameProvider.Close`; `pkg/music/player/player.go:runPlayback`.

**Evidence.** `Sink.Stream` blocks on exactly two events:

```go
select {
case err := <-provider.done:
    return err
case <-stop:
    return stream.ErrPlaybackStopped
}
```

`provider.done` is written only by `finish()`, called from `ProvideOpusFrame` (normal end / read error / stop) and from `frameProvider.Close()`. Grepping disgo v0.19.6 for any call to `OpusFrameProvider.Close()` returns **nothing**. `AudioSender.Close()` only invokes `cancelFunc`; `connImpl.Close()` does not touch the sender at all.

Consequently `ErrVoiceTransport` is produced at exactly one site in the entire repository (`sink.go:111`) and that site is dead code. Every consumer of it — `maxVoiceTransportAttempts`, `RecoveryHard`/`RecoverySoft`, `PLAYER_TRANSPORT_RECOVERY_MODE`, `PLAYER_TRANSPORT_SOFT_ATTEMPTS`, `ReopenAfterTransportFailure` — is unreachable on the disgo path.

The two tests that asserted this contract were deleted in `5953df1` and not replaced:
```
internal/discord/voice/sink/sink_discord_test.go
  TestSendOpusTimeoutIsVoiceTransport
  TestSendOpusClosedChannelIsVoiceTransport
```
[docs/architecture.md:564](docs/architecture.md) still says that file "pins down the Opus-send contract."

**Failure mode.** Bot is moved or kicked from the channel, or the voice websocket drops (routine on a throttled link). disgo closes the sender; `ProvideOpusFrame` is never called again; `Sink.Stream` blocks indefinitely; `runPlayback` never returns; `doneCh` never closes. The track never ends, the queue never advances, the status message stays on "Now Playing", and `OnPlaybackFailed` never fires. Nothing recovers it — not the WS watchdog, not `DISCORD_UNHEALTHY_MODE=restart-voice` (which closes the conn and leaves the wedge), not a session restart. Only `/stop` or `/next` unblocks it, because those close `stopCh`.

A variant is worse in a different way: when a UDP write fails with something other than `net.ErrClosed`, `defaultAudioSender.handleErr` logs and continues, so the reader is drained at 50 Hz into a dead socket and the track "plays" to completion with nobody hearing it.

**Direction.** Melodix must detect transport death itself rather than waiting for a callback disgo does not make. Options: have `Sink.Stream` also select on a ticker that checks `manager.GetConn(guildID)` identity and `conn.ChannelID() != nil`; or register a `conn.SetEventHandlerFunc` and translate voice-gateway close into `finish(ErrVoiceTransport)`. Whichever — restore the two deleted tests against a fake conn first.

---

## P1

### M-03 — The cached sink provider is bound to a dead session after a reconnect
**Severity** P1 · **Cost** high · **Confidence** high

**Location** `internal/discord/voice/service.go:GetOrCreatePlayer`, `internal/discord/session_run.go:clientConn.NewSinkProvider`.

**Evidence.** The service memoizes the provider for the process lifetime:
```go
provider, ok := s.sinkProviders[guildID]
if !ok {
    provider = s.newSinkProvider(guildID)
    s.sinkProviders[guildID] = provider
}
```
`sinkProviders` is only reset in `StopAllPlayers`, which runs on shutdown. But `newSinkProvider` captures session-scoped values:
```go
return sink.NewProvider(c.client.VoiceManager, c.dave, gid, c.voiceDelay, c.log)
```
`c.client` is the session's `*bot.Client`; `c.dave` is the `DaveRegistry` constructed fresh per `RunSession`. After an unhealthy restart, the cached provider still holds session N's `voice.Manager` and registry while session N+1 is live.

**Why it is a problem.** This is the exact scenario the architecture is designed around. `APIGetter` is a function *so the service survives reconnects*; `SinkProviderFactory` is the same idea applied and then defeated by memoization. `InvalidateAllSinks` drops the `Conn` but not the `Provider`, and the `Provider` is what is session-bound.

**Failure mode.** After a watchdog-triggered restart, the next `Sink()` calls `CreateConn` on a manager belonging to a closed client and `conn.Open` sends OP4 over a dead gateway. Join fails or times out at 15 s, three times, and playback dies with `ErrSinkUnavailable` — silently, since [architecture.md](docs/architecture.md) promises "queues and players survive reconnects."

Same shape, lower probability: a guild whose player is created while `currentConn()` is nil caches `deadSinkProvider{}` permanently.

**Direction.** Don't cache. Call `newSinkProvider(guildID)` per acquisition, or key the cache by session generation and clear it in the restart path alongside `InvalidateAllSinks`.

---

### M-04 — `Player.Stop` clobbers a newer run's state
**Severity** P1 · **Cost** high (leaks + double playback + undiagnosable) · **Confidence** medium-high

**Location** `pkg/music/player/player.go:Stop`.

**Evidence.** `runPlayback` is careful: `clearIfCurrent(track)` checks `p.currTrack == track` before resetting anything, and the comment explains why. `Stop` performs the same reset with no such guard, in a second critical section separated from the first by a wait of up to 10 s:

```go
p.mu.Lock(); doneCh = p.playbackDone; p.stopOnce.Do(...); p.mu.Unlock()
if p.IsPlaying() && doneCh != nil { <-doneCh or 10s }
p.mu.Lock()
p.playing = false; p.starting = false; p.currTrack = nil   // unconditional
...
p.stopPlayback = make(chan struct{}); p.playbackDone = make(chan struct{})
p.stopOnce = sync.Once{}
p.mu.Unlock()
```

`Stop` does not hold `playNextMu`, so `startTrack` can run entirely inside that window.

**Interleaving.** Run 1 reaches natural EOF at the moment a user runs `/next`.
- A (`/next` handler): `Stop(false)` phase 1 closes run 1's `stopPlayback`, then waits on `doneCh`.
- B (run 1's completion goroutine): `runPlayback` returns nil, `close(doneCh)` fires, A's wait returns; B proceeds to `PlayNext` → `playNextMu` → `startTrack(track2)` → mints channels C2, `currTrack=track2`, `playing=true`, spawns run 2.
- A: phase 2 executes. `playing=false`, `currTrack=nil`, and `p.stopPlayback`/`p.playbackDone` are replaced with a third pair that belongs to nobody.

**Resulting state.** Run 2 is producing audio while `IsPlaying()` reports false and `CurrentTrack()` is nil. Run 2's `stopCh` is unreachable: no later `Stop` can signal it, and `Stop` waits on a `doneCh` that never closes (10 s timeout). `/play` then sees an idle player and starts run 3 concurrently — two `runPlayback` goroutines calling `conn.SetOpusFrameProvider` on the same conn, the second silently closing the first's sender. Combined with M-02, run 2's goroutine leaks permanently, holding its `RecoveryStream`, its read-ahead goroutine, and an ffmpeg process or HTTP body.

A related lost update: `target := p.target` is captured in phase 1, so `ReleaseSink(target)` in phase 2 may name a channel that is no longer current — and `ReleaseSink` returns early when `p.currentChannelID != target`, so the bot silently stays in the voice channel after `/stop`.

**Direction.** Make `Stop` take `playNextMu` for its whole duration, or give it the same identity guard `clearIfCurrent` uses (capture `currTrack`/`playbackDone` in phase 1 and reset only if they are unchanged in phase 2).

---

### M-05 — The whole bot runs on disgo's single gateway read goroutine
**Severity** P1 · **Cost** high (architectural; silently invalidates a subsystem) · **Confidence** high

**Location** `internal/discord/session/session.go:New` — `disgo.New(...)` omits `bot.WithEventManagerConfigOpts(bot.WithAsyncEventsEnabled())`.

**Evidence.** `eventManagerConfig.AsyncEventsEnabled` defaults to false (`bot/event_manager_config.go`). `DispatchEvent` then calls each listener inline while holding `eventListenerMu`, and `HandleGatewayEvent` wraps that in `e.mu`. `gatewayImpl.listen` is one goroutine that reads the socket and calls `eventHandlerFunc` synchronously. discordgo dispatches each handler in its own goroutine unless `SyncEvents` is set; disgo is the opposite default, and the migration ported the handlers without changing the dispatch model.

Everything melodix does in a handler is therefore on the socket read loop:
- `Play.Run`: `ResolveTracks` (InnerTube/SoundCloud HTTP, up to 100-item playlist expansion with paged continuations) and `RecoveryStream.Open` (HTTP, or spawning ffmpeg).
- `onReady`: `syncer.SyncGuildCommands` per guild — one REST GET plus up to N writes, each followed by a deliberate `time.Sleep(25 * time.Millisecond)` ([cmdsync/syncer.go:107,121,134](internal/discord/cmdsync/syncer.go)).
- `WithCommandLogger` middleware: up to two REST calls per command on a cache miss.

**Why it is a problem.**
1. Discord's interaction ACK deadline is 3 seconds *from when the interaction was created*. A second interaction arriving while the first is resolving is dispatched late; `Defer()` then fails with 10062 and the user sees "The application did not respond."
2. `COMMAND_PARALLELISM` (default 16) and the entire `execguard` semaphore cannot do anything — commands are already serialised, so the semaphore never contends. A configuration knob and a package exist that describe a property the runtime does not have.
3. `cmdsync`'s `perGuildLocks sync.Map` likewise can never contend.
4. During a long command, no `events.Raw` reaches `tracker.MarkWSNow()` and no `events.HeartbeatAck` reaches `Session.onHeartbeatAck`. `WS_SILENCE_TIMEOUT` defaults to 2 minutes so this rarely fires, but the watchdog is measuring a signal the bot itself is now capable of starving — a structurally similar failure to the wedged-mutex one the migration set out to eliminate.

**Direction.** Either enable async events and let `execguard` do the job it was written for, or keep sync dispatch and move command execution onto its own goroutine in `runGuardedInteraction`. The second is safer (it keeps event ordering for `Ready`/`GuildJoin`) and is a two-line change. Whichever, `time.Sleep` in `cmdsync` should go — disgo's REST client has a rate limiter.

---

### M-06 — Voice connection state is mirrored with no reconciliation
**Severity** P1 · **Cost** high · **Confidence** medium-high

**Location** `internal/discord/voice/sink/provider.go` (`p.conn`, `p.currentChannelID`).

**Evidence.** The provider decides whether it is connected from its own fields:
```go
if p.conn != nil && p.currentChannelID == target {
    return &Sink{conn: p.conn, log: p.log}, nil
}
```
It never asks `p.manager.GetConn(p.guildID)`. But disgo removes conns on its own: `connImpl.handleGatewayClose` calls `c.Close(ctx)` → `removeConnFunc()` → `manager.RemoveConn(guildID)`, with no notification to melodix.

**Failure mode.** The voice gateway drops (common on a lossy link). disgo tears down and deregisters the conn. `p.conn` stays non-nil, so the next track's `Sink()` hands back a `Sink` wrapping a dead conn; `SetOpusFrameProvider` on it starts a sender whose UDP writes fail — straight into the M-02 wedge. Separately, `DaveRegistry.Forget` is never called on this path, so the guild's `davesession.Session` is never `Close()`d; its own comment notes that dave-go "arms recovery watchdogs that keep re-arming invalidations on a channel the bot has left." One leaked session with live timers per unclean disconnect.

**Direction.** Make `manager.GetConn(guildID)` the single source of truth — compare identity on every `Sink()` and treat a mismatch as "not connected". Alternatively register a `removeConn` hook in `ManagerOptions` (the create func already closes over the guild id) that clears `p.conn` and calls `Forget`.

---

### M-07 — `*parsers.Track` is shared mutable state with no owner
**Severity** P1 · **Cost** medium-high (real race; silent wrong UI) · **Confidence** high

**Location** `pkg/music/stream/recovery.go:Open`, `pkg/music/player/player.go:CurrentTrack`, `internal/discord/reply/musicstatus.go:trackChips`.

**Evidence.** `RecoveryStream.Open` writes into the shared track:
```go
rs.track.Passthrough = false
rs.track.Cached = false
...
rs.track.CurrentParser = parser
```
`Open` is reachable from `ReadPacket`, which under `BUFFER_AHEAD_MS > 0` (default 30000) runs on the `opus.BufferedReader` read-ahead goroutine. Meanwhile `watchPlayerStatus` calls `p.CurrentTrack()` and passes the same pointer to `reply.NowPlayingEmbed`, which reads `track.Cached`, `track.CurrentParser` and `track.SourceInfo.SourceName`.

`p.mu` protects the *pointer*, not the pointee. The author was aware of the aliasing — `cloneTrack` exists and is used for the recorder and for `failedSnapshot` — but the UI path reads the live object.

**Failure mode.** Unsynchronised read/write on every mid-track parser switch, which is exactly the case `now_playing_parser_corrected` exists to render. `-race` does not catch it because no test drives a parser switch and a status render concurrently.

**Direction.** Have `CurrentTrack()` return `cloneTrack(*p.currTrack)`, or make the confirmation callback carry an immutable snapshot rather than the pointer.

---

### M-08 — `ReopenAfterTransportFailure` races the read-ahead producer over `RecoveryStream`'s private fields
**Severity** P1 (latent) · **Cost** high if triggered (`fatal error: concurrent map writes`) · **Confidence** medium-high

**Location** `pkg/music/stream/recovery.go`.

**Evidence.** The field comment states the contract:
> "mu guards reader and cleanup — the only fields Close touches while the read-ahead producer may still be running. Every other field belongs to the producer goroutine alone."

That holds for `Close`, which does `Stop() → closeCurrent() → Wait()` before touching anything else. It does **not** hold for `ReopenAfterTransportFailure`:
```go
func (rs *RecoveryStream) ReopenAfterTransportFailure() error {
	rs.closeCurrent()
	return rs.Open(rs.seekSec)
}
```
This is called from `runPlayback` (player goroutine) with no `Stop`/`Wait`, by design — the whole point of the buffer sitting above recovery is that it keeps running across a transport reopen. `Open` writes `parserIndex`, `seekSec`, `curParser`, `firstRead`, `fromCache`, `cacheDisabled`, `cacheWriter`, and reads/writes the `retries` map, all of which `ReadPacket` on the producer goroutine also touches.

**Why it has not fired.** `ReopenAfterTransportFailure` is only reachable from the `ErrVoiceTransport` branch, which M-02 makes unreachable. One bug is masking the other. **Fixing M-02 activates this race**, and a concurrent write to `rs.retries` is a runtime throw, not a recoverable error.

**Direction.** Fix these two together. Either `Stop()/Wait()` the buffer around the reopen (losing the lead, which defeats its purpose), or extend `rs.mu` to cover the reopen-mutated fields and take it in `ReadPacket` around the recovery branches.

---

### M-09 — `ReleaseSink` runs under `player.mu`, for up to ~25 seconds
**Severity** P1 · **Cost** medium-high · **Confidence** high

**Location** `pkg/music/player/player.go:Stop`.

**Evidence.**
```go
p.mu.Lock()
...
if disconnect {
    p.sinkProvider.ReleaseSink(target)   // blocking
}
...
p.mu.Unlock()
```
`ReleaseSink` takes `Provider.mu`, which `Sink()` holds across `conn.Open` (`voiceJoinTimeout` 15 s) + `time.Sleep(voiceReadyDelay)` + `WaitReady` (`daveReadyTimeout` 10 s), then does `conn.Close(ctx)` bounded at `voiceCloseTimeout` 10 s.

**Failure mode.** Worst case ~25 s holding `player.mu`, during which `IsPlaying`, `CurrentTrack`, `Queue`, `EnqueueTrackInfos`, `clearIfCurrent` and `onParserConfirmed` all block. Under M-05's synchronous dispatch, that is the entire bot for every guild.

**Direction.** Capture `target` and the provider under the lock, release the lock, then call `ReleaseSink`.

---

### M-10 — The voice/DAVE/sink layer has no tests, and the docs claim otherwise
**Severity** P1 · **Cost** high (this is why M-01/M-02 are invisible) · **Confidence** high

**Evidence.** `internal/discord/voice`, `internal/discord/voice/sink` and `internal/discord/session` have zero `_test.go` files. `5953df1` deleted `sink_discord_test.go` (3 tests) and `provider_discord_test.go` (4 tests) and replaced neither. [docs/architecture.md:564](docs/architecture.md) still asserts `sink_discord_test.go` "pins down the Opus-send contract: stop unblocks a stalled send, and a stalled or closed channel produces `ErrVoiceTransport`" — a property that is now false.

Meanwhile `internal/conventions` carries ~1,150 lines of enforcement whose ratcheted rules are comment width, log-event naming, error prefixes and file headers.

**Direction.** Port `TestSendOpusTimeoutIsVoiceTransport` and `TestSendOpusClosedChannelIsVoiceTransport` against a fake `voice.Conn` before touching anything else — they are the regression tests for M-02. Then correct the doc.

---

## P2

### M-11 — `COMMAND_TIMEOUT` enforces nothing
`execguard.Guard.Context` builds a `context.WithTimeout`, `runWithCommandContext` passes it to `fn`, and `Adapter.Run` drops it on the floor:
```go
func (a *Adapter) Run(ctx context.Context, inv *command.Invocation) error {
	return a.Cmd.Run(inv.Data)
}
```
`Handler.Run(ctx interface{})` takes the *invocation data*, not a context — and the parameter is still named `ctx`. No REST call anywhere in the repo passes `rest.WithCtx`, and no engine call takes a context. So the timeout is observed after the fact (`errors.Is(cmdCtx.Err(), DeadlineExceeded)`) and used only to relabel a slow command's error message. **Direction:** either give `Handler.Run` a real `context.Context` and thread it into `resolve`/`stream`/REST, or delete the knob and say so.

### M-12 — A track enqueued at queue end is silently destroyed
`PlayNext` observes an empty queue, releases `p.mu`, returns `ErrNoTracksInQueue`; the completion goroutine then calls `Stop(true)` which does `p.queue = nil`. A `/play` that appends between those two points loses its tracks and gets "Nothing is in the queue to play." The window is small but lands exactly when a user reacts to "Playback Finished". **Direction:** have `Stop(true)` clear the queue only if it is still empty, or move the queue-end decision inside `playNextMu`.

### M-13 — `IntentsAll` plus `cache.FlagsAll`
`gateway.WithIntents(gateway.IntentsAll)` requests all three privileged intents (`GuildMembers`, `GuildPresences`, `MessageContent`); a gateway will close with 4014 if any is not enabled in the developer portal. `IntentsAll` predates the migration, but `cache.WithCaches(cache.FlagsAll)` does not — it adds `FlagMessages`, `FlagPresences`, `FlagMembers`, `FlagStickers`, `FlagGuildScheduledEvents`, `FlagGuildSoundboardSounds` for a bot that reads `Guild`, `Channel`, `Member`, `SelfMember`, `SelfUser` and `VoiceState` and nothing else. **Direction:** request `IntentGuilds | IntentGuildVoiceStates` (plus `IntentGuildMessages` only if M-14 is kept), and cache `FlagGuilds | FlagChannels | FlagRoles | FlagMembers | FlagVoiceStates`.

### M-14 — `onMessageCreate` is a dead discordgo-era dispatch path
It loops every registered command and runs each with a `*cmdadapter.MessageContext`. No command in the repository handles that type — every `Run` begins with a type assertion to `*SlashInteractionContext` and returns nil on failure. So the handler burns a guard slot, iterates the registry, and does nothing. `MessageReactionContext` and `ReactionProvider` are similarly producerless. **Direction:** delete the mention path (it is the only reason `MessageContent` is needed), or give it one command that actually handles the context. `Unlogged` should stay — it is documented as shared with another project.

### M-15 — `alreadyAcknowledged` matches Discord's English error text
```go
return strings.Contains(err.Error(), "already been acknowledged")
```
disgo exposes `rest.Error{Code: rest.JSONErrorCodeInteractionAlreadyAcknowledged}` (40060) with an `Is` method. The substring match works today only because `(*rest.Error).Error()` renders `"40060: Interaction has already been acknowledged."`. The comment even says "Matched on text because the API returns it as a generic error" — true of discordgo, not of disgo. Every response fallback in `Responder` depends on this. **Direction:** `errors.Is(err, &rest.Error{Code: rest.JSONErrorCodeInteractionAlreadyAcknowledged})`.

### M-16 — `atomic.Value` + holder structs where `atomic.Pointer[T]` fits
`bot_state.go` stores three `atomic.Value`s, each boxing a pointer-to-struct wrapper, each read through a defensive `v.(*holder)` assertion plus two nil checks. `atomic.Pointer[context.Context]`/`[execguard.Guard]`/`[conn]` (Go ≥1.19; module targets 1.26) removes the wrappers, the assertions and the nil checks. The tell is `bot_atomic_test.go`, which asserts that storing the same type twice into an `atomic.Value` does not panic — a test of the standard library, written because the pattern's failure mode was hit. **Direction:** convert; delete the test.

### M-17 — Global mutable state in the "reusable" engine
`stream.SetCache`, `stream.SetBufferAhead`, `stream.SetRegistry` (atomic, swapped by tests), plus `kkdai.SetLogger`/`ffmpeg.SetLogger`/`soundcloudapi.SetLogger`/`ytnative.SetLogger`/`ytdlp.SetLogger`/`ytnative.SetMaxBitrate` — all package-level. Worst of them:
```go
// pkg/music/parsers/kkdai/streamer.go
func init() { youtube.DefaultClient = VisionOSClient }
```
Importing a melodix parser silently rewrites a third-party package's global for the whole process. The engine is a singleton in practice, which sits oddly with the claim that `pkg/music` is reusable on its own terms. This is consistently applied, so it is debt rather than chaos — but `init()` reaching into someone else's package is a footgun for any future embedder.

### M-18 — `Run(ctx interface{})` with silent-nil assertions
Every command opens with
```go
slashCtx, ok := ctx.(*cmdadapter.SlashInteractionContext)
if !ok { return nil }
```
A command dispatched with the wrong context type succeeds and does nothing. The same shape recurs throughout `reply_methods.go` (`if r == nil { return nil }` in a dozen helpers). Combined with M-14 this is the mechanism by which an entire dispatch path is dead without a single log line. **Direction:** at minimum return an error rather than nil on a context-type mismatch, so the dispatcher's `onError` fires.

---

## P3

- **M-19 — `sink` is two packages, `Provider` is two things.** `pkg/music/sink` (interface `Provider`, interface `AudioSink`) and `internal/discord/voice/sink` (struct `Provider`, struct `Sink`). Every importer of both writes `musicsink "..."`. Also: `internal/discord/voice/sink/stream.go` contains no stream — it holds warm-up constants.
- **M-20 — Reply errors are not handled.** In `internal/command/`: 26 bare ignored `Followup*`/`Respond*` calls, 1 explicitly discarded with `_ =`, 0 checked. `play.go` ignores them bare; `stop.go` writes `_ =` for the same call. Pick one.
- **M-21 — `SessionLockWedged` is a dead concept.** `WSSilenceMeta.SessionLockWedged` and the `(time.Time, bool)` shape of `LastHeartbeatAck` exist because the fork could fail to *read* the ACK. disgo cannot; `session_run.go` hardcodes `return ack, true` with a five-line comment explaining why. Carry the simplification through the watchdog instead of documenting a branch that cannot be taken.
- **M-22 — `cmdsync` synchronises against nothing.** `perGuildLocks sync.Map` and `time.Sleep(rateLimitDelay)` are both redundant: sync dispatch makes concurrent sync impossible, and disgo's REST client rate-limits.
- **M-23 — `execguard.Release` silently drains.** The non-blocking `select { case <-g.sem: default: }` means an unpaired Release quietly frees someone else's slot instead of surfacing the bug.
- **M-24 — Stale comments as documentation.** `VOICE_READY_DELAY_MS` is described in [config.go](internal/config/config.go) as covering a "discordgo op 4 race"; it now covers the SELECT_PROTOCOL_ACK gap, and the code comment in `awaitEncryption` concedes it "is a delay, not a synchronisation." [docs/dave-todo.md](docs/dave-todo.md) says "The current branch does not set it at all and uses the library default" while `dave_registry.go` sets `DaveRecoveryTimeout = 5 * time.Second`.
- **M-25 — `playersStopTimeout` is one budget for all guilds.** `closeWithin("stop_players", 10s, …, b.stopAllPlayers)` wraps a sequential loop where each `Stop(true)` can itself wait 10 s. With one wedged guild (M-02), no later guild is stopped and the bot leaves voice channels dirty.

---

# Race/concurrency findings

Ordered by severity. Every shared mutable state I could trace:

| State | Owner | Writers | Readers | Protection | Verdict |
|---|---|---|---|---|---|
| `Player.queue/currTrack/playing/starting/target` | player | gateway goroutine (commands), completion goroutine, runPlayback | all three + status watcher | `p.mu` | **M-04**: `Stop`'s reset is unguarded by track identity |
| `Player.stopPlayback/playbackDone/stopOnce` | per-run | `startTrack`, `Stop` | `Stop`, runPlayback | `p.mu` | **M-04**: can be replaced out from under a live run |
| `*parsers.Track` fields | nobody | `RecoveryStream.Open` (producer goroutine) | `NowPlayingEmbed` (status goroutine) | **none** | **M-07**: confirmed race |
| `RecoveryStream.{seekSec,firstRead,retries,parserIndex,curParser,cacheWriter,fromCache}` | producer goroutine | producer + player goroutine via `ReopenAfterTransportFailure` | both | `rs.mu` covers only `reader`/`cleanup` | **M-08**: latent, incl. concurrent map write |
| `RecoveryStream.reader/cleanup` | — | `setActive`, `closeCurrent` | `ReadPacket` | `rs.mu` | correct |
| `BufferedReader.err` | producer | producer | consumer | published by `close(b.pkts)` | correct |
| `Provider.conn/currentChannelID` | provider | `Sink`, `releaseLocked` | same | `p.mu` | **M-06**: mirrors disgo state that changes behind it; **M-09**: held across ~25 s of I/O |
| `DaveRegistry.sessions` | registry | ConnCreateFunc hook (under manager's `connsMu`), `Forget` | `Session` | `r.mu` | correct — and the "never call into the session under a connection lock" rule is honoured |
| `Service.players/sinkProviders` | service | `GetOrCreatePlayer`, `StopAllPlayers` | `InvalidateAllSinks` | `s.mu` | correct locking, wrong lifetime (**M-03**) |
| `Service.guildMusicStatus/NotifyChannel` | service | interaction goroutine | status + failure goroutines | `guildMusicStatusMu` | correct |
| `Bot.sessionCtx/cmdGuard/conn` | bot | `RunSession` | gateway goroutine | `atomic.Value` | correct, over-engineered (**M-16**) |
| `Session.lastHeartbeatAck` | session | HeartbeatAck listener | watchdog goroutine | `s.mu` RWMutex | correct |
| `Tracker.lastWSNano/readyNano` | tracker | Raw/Ready listeners | watchdog | atomics | correct |
| `syncer`/`logger` locals in `RunSession` | RunSession | RunSession | gateway listeners | none | safe **only** because the writes precede `session.Open`, which creates the reader goroutine. Undocumented happens-before dependency. |
| `connImpl.audioSender` (disgo) | disgo conn | gateway goroutine (`HandleVoiceStateUpdate`) and player goroutine (`SetOpusFrameProvider`) | both | **none in disgo** | upstream race that melodix's per-track provider swap exposes |
| `defaultAudioSender.cancelFunc` (disgo) | disgo | the sender goroutine | `Close()` from another goroutine | **none** | upstream race; `Close()` before the goroutine assigns it is a nil deref. Melodix calls `SetOpusFrameProvider` twice per track, which is the window. |

Not found: deadlocks in melodix's own code (the DAVE lock-order rule from the fork era is respected — `DaveRegistry` only stores pointers). Goroutine leaks: one per wedged run (M-02/M-04), plus one leaked nil-provider `defaultAudioSender` per `Conn` ever created, since `connImpl.Close` does not close the sender and the sender spins at 50 Hz forever.

---

# DAVE/playback audit

**Reconstructed lifecycle.**

| Object | Created | Destroyed | Scope |
|---|---|---|---|
| Gateway session | `RunSession` → `session.New` + `Open` | `RunSession` defer | per restart |
| `DaveRegistry` | `RunSession` | never (GC with the session) | per gateway session |
| `sink.Provider` | first `GetOrCreatePlayer` for the guild | `StopAllPlayers` (shutdown only) | **process lifetime** ← mismatch |
| `voice.Conn` | `Provider.Sink` → `manager.CreateConn` | `releaseLocked`, or disgo's `handleGatewayClose` | per join |
| `davesession.Session` | inside `voice.NewConn` via `ManagerOptions`' `WithConnCreateFunc` | `DaveRegistry.Forget` in `Provider.removeConn` | per `voice.Conn` |
| `player.Player` | first `GetOrCreatePlayer` | `StopAllPlayers` | per guild |
| queue | player construction | `Stop(true)` | per guild |
| `RecoveryStream` | `startTrack` | `runPlayback` defer | per track |
| `frameProvider` | `Sink.Stream` | GC | per track |

**What is correct.** The DAVE session is per voice-connection, which is the right granularity: it is *not* recreated per track, it survives queue transitions and `Sink()` reuse, and it is not shared between guilds. Using `WithConnCreateFunc` to capture the session by construction, rather than a pending field read back after `CreateConn`, is a genuinely good design — the comment explaining why is accurate. Using `dave-go` rather than `disgoorg/godave`'s cgo session to keep `CGO_ENABLED=0` cross-compilation is a correct, load-bearing call. disgo handles `SetChannelID`, `AddUser`/`RemoveUser` (opcode 11/13) and `AssignSsrcToCodec` itself, so three of the five bugs the migration document lists came for free.

**Where the lifecycles are wrongly coupled.**

1. **Registry (per session) vs Provider (per process).** The Provider caches a pointer to a registry that dies with its gateway session — M-03.
2. **`voice.Conn` vs `Provider.conn`.** Two records of one fact, with disgo authoritative and melodix never asking — M-06. This also leaks the DAVE session on the path disgo controls.
3. **Encryption readiness vs track boundary.** `awaitEncryption` runs on join only. Track 2..N of a queue take the `p.conn != nil && p.currentChannelID == target` fast path and are never gated. More importantly, readiness is not a join-time property at all: it is re-established on every re-key. Gating once at join and never on the send path is the core error — M-01.
4. **Transport death vs track end.** `Sink.Stream` conflates "the provider finished" with "the track ended"; there is no third outcome for "the connection went away", because the callback that was supposed to deliver it is never invoked — M-02.

**Answers to the specific questions asked.** DAVE state lives in `davesession.Session`, owned by disgo's `voice.Conn` and mirrored (pointer only) in `DaveRegistry`; it is initialised inside `voice.NewConn`, mutated by disgo's voice gateway handlers, and destroyed by `Forget`. It is per-guild-per-voice-connection. It survives queue transitions (correct) and does **not** survive voice reconnects (correct, a new `Conn` means a new MLS group). It is not recreated per track and not shared between players. Queue transitions can occur while DAVE state is changing, and nothing coordinates them — that is M-01's failure window. It does not match disgo's expectations in one respect: the `godave.Session` doc requires the audio sender to hold on `!Ready()` and neither disgo nor melodix does. Encryption state is not confused with track state anywhere. Cleanup happens at the right boundary on the paths melodix controls, and not at all on the path disgo controls.

---

# Migration leftovers

Syntactically migrated, semantically still discordgo:

1. **Sync vs async event dispatch** (M-05) — the single largest one. discordgo's default is a goroutine per handler; disgo's is inline. The handlers were ported as-is.
2. **`frameProvider`'s push-model warm-up** — `warmUpFrames = 10` exists "to prime the upstream pipeline before the 20ms-paced send begins." In disgo's pull model the sender is already running, so up to 160 blocking `ReadPacket` calls now happen *inside* the first 20 ms tick. Harmless in effect, but the comment describes a model that no longer exists.
3. **`frameProvider.Close` as the transport-failure signal** (M-02) — modelled on the fork's push loop, where the sink detected the failure itself.
4. **The lost frame-hold** (M-01) — implemented on the fork's `voice.go` send path, deleted with it, not reimplemented.
5. **`alreadyAcknowledged` string matching** (M-15) — the comment names discordgo's generic errors as the reason; disgo has typed codes.
6. **`WSSilenceMeta.SessionLockWedged`** and the `bool` from `LastHeartbeatAck` (M-21) — a fork-specific failure mode preserved in the type system.
7. **`internal/discord/session`** — disgo has no "session"; it has a `Client`. The package name and the `Session` type are discordgo vocabulary wrapping a `*bot.Client`.
8. **`VOICE_READY_DELAY_MS`'s "(discordgo op 4 race)"** (M-24).
9. **`onMessageCreate`** (M-14) — mention/prefix dispatch that no command consumes.
10. **`cmdsync`'s manual 25 ms sleep** (M-22).
11. **`-check-disgo`** — scaffolding with a demolition date, and the date has passed: the migration is complete and the fork is gone.
12. **Deleted tests not replaced** (M-10, M-02) — `sink_discord_test.go`, `provider_discord_test.go`.

Correctly migrated and worth noting as such: the `godave.SessionCreateFunc` seam crossed over unchanged, as the document predicted; `ApplicationID` no longer needs the `User("@me")` fallback; the two watchdogs correctly collapsed into one.

---

# Pattern consistency

**Where it is systematic (a real "Melodix way"):**
- **Adapter boundary.** One rule, mechanically enforced, honoured everywhere. This is the strongest thing in the repo.
- **Neutral contexts.** Five context types, all built in the root handlers, all carrying `Invoker`/`Responder`/`API` and no library value. Uniform.
- **Optional capabilities by interface assertion.** `SlashProvider`, `ContextMenuProvider`, `ComponentInteractionHandler`, `Unlogged` — one mechanism, forwarded through `Adapter`, with compile-time `var _ =` proofs. Consistent and well-reasoned.
- **Logging.** `zerolog`, snake_case event names as `Msg()`, structured fields, enforced by a convention rule. Uniform across ~60 files.
- **Error sentinels.** `errors.New` at package level, `errors.Is` at use sites, `%w` wrapping. Consistent.
- **Frozen identifiers.** Parser keys and component-id formats, pinned by `TestFrozenIdentifiers`. Genuinely good.

**Where it is a zoo:**
- **Context propagation.** Three incompatible models coexist: `execguard` builds a real `context.Context` that nothing consumes (M-11); `Handler.Run(ctx interface{})` names the *data* `ctx`; the engine takes no context at all and uses hardcoded timeouts (`voiceJoinTimeout`, `daveReadyTimeout`, `voiceCloseTimeout`, `10 * time.Second` in `Stop`). There is no canonical answer. **Canonical should be:** a real context threaded from the dispatcher to the engine; timeouts become deadlines.
- **Reply error handling** (M-20) — 26/1/0. **Canonical should be:** log-and-continue at one helper, not at 27 call sites.
- **Lifetime of session-scoped objects.** `APIGetter` re-asks every call; `SinkProviderFactory` is memoized once (M-03); `reply.NewSessionAPI(client)` is reallocated on every `clientConn.API()` call. Three different answers to one question. **Canonical should be:** re-ask, as `APIGetter` does.
- **Concurrency primitives.** `sync.Mutex`, `sync.RWMutex`, `atomic.Value`+holder, `atomic.Pointer`, `atomic.Bool`, `atomic.Int64`, `sync.Map`, `sync.Once`, channels. Most choices are locally defensible, but `atomic.Value`+holder (M-16) and `sync.Map` for a single-goroutine map (M-22) are not.
- **Test investment.** ~1,150 lines enforcing comment width and log-event naming; zero lines covering voice, DAVE, sinks or session lifecycle. The ratchet is a good mechanism pointed at the cheap problems.

---

# Naming consistency

Only the ones that make the architecture harder to read:

1. **Two packages named `sink`.** `pkg/music/sink` and `internal/discord/voice/sink`. Every file importing both must alias (`musicsink`). Worse, each exports a `Provider` — one an interface, one its implementation — and a `Sink`/`AudioSink` pair. Reading `sink.Provider` requires knowing which file you are in. **Rename the Discord one to `voicesink`, or the type to `DiscordProvider`.**
2. **`session.Session` wrapping `*bot.Client`.** disgo has no session concept; "session" is discordgo's word, and it also collides with the *voice* session, the *DAVE* session, and the `sessionCtx`. At least four meanings of "session" in one package tree.
3. **`cmdadapter.Logger`** is the audit-record interface, not a logger. The accessor is honestly named `AuditLogger()`; the type is not. It sits alongside `cmdlogger.Logger` and `zerolog.Logger`.
4. **`ctx` for a non-context.** `Handler.Run(ctx interface{})`, `Play.Run(ctx interface{})`, `Join(bot, ctx cmdadapter.Interaction)`. In a codebase that also passes real `context.Context`, this actively misleads — and M-11 is the bug it hides.
5. **"Registry" means two things.** `stream.registry` (parsers), `command.DefaultRegistry` (commands), `DaveRegistry` (MLS sessions).
6. **`conn`, `clientConn`, `voice.Conn`, `connHolder`, `deadSinkProvider`** — five connection-ish names in `internal/discord`, three of them in one file.
7. **`stream.go` in `voice/sink/`** contains no stream, only warm-up constants.

Not problems: `Streamer` as one interface with five implementations; `Provider`/`Service`/`Syncer`/`Tracker`/`Guard` each carry a distinct meaning here; `Bot`, `Player`, `RecoveryStream`, `BufferedReader` are all accurate.

---

# Recommended fix order

1. **Restore the two deleted sink tests** against a fake `voice.Conn`. They are the harness for everything below (M-10).
2. **M-02** — make transport death observable. Without this the player's recovery is decorative.
3. **M-08** — fix the `ReopenAfterTransportFailure` race *in the same change*, because fixing M-02 activates it.
4. **M-01** — gate `ProvideOpusFrame` on `ShouldHoldFrames()`. Security property, and the regression the project already paid to fix once.
5. **M-03** — stop caching the sink provider. One-line class of fix, restores the advertised reconnect behaviour.
6. **M-06** — make `manager.GetConn` the source of truth; clear `p.conn` and `Forget` on disgo-initiated removal.
7. **M-04** — give `Stop` the identity guard `clearIfCurrent` already has, and **M-09** — move `ReleaseSink` out from under `p.mu`. Same function, one change.
8. **M-05** — run commands off the gateway goroutine (or enable async events). Cheap, and it makes `execguard` mean something.
9. **M-07** — clone the track in `CurrentTrack()`.
10. **M-13** — narrow intents and cache flags. Pure win, no design cost.
11. **M-11** — decide whether `COMMAND_TIMEOUT` is real; thread a context or delete the knob.
12. **M-14** — delete the mention path; **M-12**, **M-15**, **M-16**, **M-17**'s `init()`.
13. **M-19** naming, **M-20** error handling, and the remaining P3s — a cleanup pass, not a project.

---

# What is already good

- **The adapter boundary and its enforcement.** `TestLibraryStaysDiscordFree` and `TestDiscordStaysBehindTheAdapter` turn an architectural intention into a build failure. `cmd/cli` is a real proof, not a claim. This is the reason the disgo migration was possible at all, and it should be protected.
- **`TestDocumentAndChecksAgree`.** A rule tagged as enforced in the doc must have a check, and vice versa. That is a better idea than most of the rules it governs.
- **The `conn` interface.** Two methods, replaced wholesale, never half-torn. The reasoning in its doc comment is correct and the design follows it.
- **`DaveRegistry`'s `WithConnCreateFunc` approach.** Filing the session by construction rather than via a pending field is the right answer to a genuinely awkward API gap, and the comment explains both the problem and the deadlock rule accurately.
- **`closeWithin` and per-phase shutdown timing.** "voice_close took 9.9 s and everything else took 40 ms" is the line you cannot have unless you wrote it in advance. Keep it.
- **`RecoveryStream.Close`'s ordering.** `closed → Stop → closeCurrent → Wait → abortCache` is subtle and correct, and the comment says why each step is where it is.
- **The anti-skip buffer above recovery rather than below**, and the "Tried and rejected" section of [architecture.md](docs/architecture.md) that records why. Writing down the reverted experiments is rarer than it should be.
- **`cloneTrack` for the recorder and the failure snapshot** — the aliasing hazard was seen; it just was not followed to the UI path.

---

# Confidence / unknowns

**High confidence (verified against library source):** M-01, M-02, M-05, M-10, M-13, M-15 — each rests on reading disgo/godave/dave-go source in the module cache, not on inference. M-03, M-11, M-14 are direct reads of melodix's own control flow.

**Medium-high:** M-04, M-06, M-07, M-08, M-09. The mechanisms are certain; the trigger probabilities are estimated, not measured.

**Could not establish statically:**
- Whether the bot's Discord application actually has the three privileged intents enabled. If it does, M-13 is a memory concern only; if it does not, the gateway is refused with 4014.
- Real-world frequency of mid-track voice drops on the owner's link, which sets how often M-02 fires. `docs/dave-todo.md` suggests it is not rare.
- Whether the upstream disgo races (`connImpl.audioSender`, `defaultAudioSender.cancelFunc`) fire in practice. They need a `-race` run against a live voice connection, which the test suite cannot do.
- How long `ResolveTracks` actually blocks on a throttled link for a 100-item playlist — this sets whether M-05 produces occasional 10062s or routine ones.

**What would settle these:** a `-race` build run against a real guild doing (a) `/play` a 100-track playlist, (b) a listener joining and leaving an E2EE channel mid-track while watching for `PassthroughFrames`, (c) the bot being dragged out of the voice channel mid-track, (d) `/next` repeatedly at track boundaries, (e) a forced session restart (`WS_SILENCE_TIMEOUT=10s`) with playback in progress. (b) and (c) are the two that matter most; neither is reachable from the test suite as it stands.

---

I did not modify anything. If you'd like this as a shareable page rather than terminal scrollback, say the word and I'll publish it as an artifact.