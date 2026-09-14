# Migrating from the discordgo fork to disgo

Working document, 14 September 2026. Written at the start of the work, so most
of it is plan rather than record. Update it as things turn out differently,
and delete it when the migration lands.

## Why

`pkg/discordgo-fork-dev` is a vendored copy of `bwmarrin/discordgo` v0.29.0
with about 2,240 lines of local divergence, wired in with a `replace`
directive. It receives no upstream fixes, and upstream is not moving — v0.29.0
is the latest release and the issue tracker has been accumulating pull requests
for a long time. So the divergence only grows, and nothing rebases it.

What the divergence actually is:

| | lines |
| --- | --- |
| `voice.go`, effectively a full rewrite | ~1,620 of 1,609 |
| DAVE glue (`dave_inject.go`, `dave_callbacks.go`) | 301 |
| `wsapi.go` | 192 |
| `structs.go` | 63 |
| `event.go`, `restapi.go` | 9 |

Only about 150 of those `voice.go` lines even mention DAVE. The rest is a
reworked voice connection lifecycle — `context.Context` throughout where
upstream has none, `sync.Cond` for status, a `Dead` channel, `seqAck` for
resume, `waitUntilStatus`. That is the part that costs something to own, and
owning it has a measured price.

**The price, measured.** Adding a real DAVE implementation on 14 September
turned up five bugs in one day, and every one of them was in the voice layer,
and disgo already handles every one of them:

| Bug | What disgo does |
| --- | --- |
| `AssignSsrcToCodec` never called — one error per frame, silence | calls it during UDP IP discovery |
| membership never fed to the DAVE session, so it would not commit | drives `AddUser`/`RemoveUser` from the gateway |
| handled opcode 12 for client-connect; Discord sends **11** | reads `OpcodeClientsConnect` (11) |
| deadlock: asked the session a question while holding the connection lock | its `Conn` has no such lock to invert |
| reused connection kept the previous channel's state | builds a fresh `Conn` per channel |

The opcode-12 one is the shape of the whole problem: the fork was handling a
message Discord had moved on from, and nothing would ever have said so.

## What this migration is not

`pkg/music` — the entire playback engine, parsers, cache, stream handling — has
**zero** references to discordgo. It is already Discord-agnostic behind
`sink.AudioSink` and `sink.Provider`. None of it is touched.

`cmd/` has zero references too.

## The surface

**Phase 1 is done, and it measured imports.** Nothing outside
`internal/discord` imports discordgo: 243 references between them at the start,
none now. That is true and it still holds.

It was not the whole boundary. An import is not needed to hold a library
value: `s := slashCtx.Session` yields a `*discordgo.Session` with nothing to
import, because the field's type is inferred. Two dozen call sites went on
reaching through `.Session` and `.Event` while the packages they lived in
counted clean -- `next.go` called `slashCtx.Defer()` two lines from
`perm.CheckBotVoicePermissions(s, ...)`. Phase 1 wrote `CanJoinVoice`; nothing
switched to it. All but four of those sites were calling for a method phase 1
had already written and nobody used.

Closing them is what made phase 2 sliceable, and `adapter-boundary` in
`internal/conventions` now fails the build on the import half. The field half
is prevented structurally instead: the contexts hold no library value to hand
out.

| Package | at the start | now |
| --- | --- | --- |
| `internal/command` | 171 | 0 |
| `internal/middleware` | 64 | 0 |
| `internal/readme` | 8 | 0 |
| `pkg/music`, `cmd` | 0 | 0 |
| `internal/discord` | 246 | 424 |

The adapter's count went up, which is the point: the dependency moved inward
rather than away. What used to be spread across 53 files is now in one package,
and phase 2 replaces it there.

What carries the seam, all in `internal/discord/cmdadapter`:

| Type | What it replaced |
| --- | --- |
| `CommandContext` | five type switches over `.Session` and `.Event` |
| `Embed`, `EmbedField` | `discordgo.MessageEmbed` and its nested structs |
| `SlashCommand`, `SlashOption`, `SlashChoice` | `discordgo.ApplicationCommand` |
| `SlashArgument` | `ApplicationCommandData().Options` loops |
| `Button`, `ActionRow` | `discordgo.Button`, `ActionsRow` |
| `Interaction` | helpers taking `(s, e)` |

`perm` owns the permission constants: `perm.Administrator`, `perm.Name(bit)`,
`perm.RecommendedBotMask()`.

Translation happens in exactly two places. `Adapter` renders a declaration for
`cmdsync`, and `reply` renders everything that goes on the wire -- its
functions take the declaration and shadow it back to the wire type on their
first line, so their bodies never changed.

### The escape hatch is gone

There was one, `cmdadapter.Interaction.Raw`, and it existed for a single
caller. `UpdatePlaybackStatus` took a session and an interaction because the
guild's music status message is created from an interaction and then edited for
as long as the track plays -- past the token's expiry, so editing cannot go
through the interaction at all.

It needed two capabilities rather than the raw pair. Creating is
`Interaction.FollowupEmbedMessage`, which posts a followup and reports where it
landed. Editing is the session, and the voice service already held a
`SessionGetter` for exactly that -- the asynchronous paths were calling it a few
lines away and passing the result back in. The signature is
`UpdatePlaybackStatus(from, guildID, embed)` now, where a nil interaction is the
asynchronous case.

Nothing outside `internal/discord` names discordgo, by any route.

## Fork-only APIs that need an answer first

These exist nowhere but our fork, so they need a replacement before anything
can compile against disgo:

- `ErrVoiceE2EERequired` (3 uses) — the sentinel the player checks to report a
  channel it cannot join.
- `VoiceConnection.WaitForDAVEReady(ctx)` (2 uses) — the gate that holds a
  track until encryption is up. disgo exposes readiness through the DAVE
  session; the session is reachable via dave-go's create hook, which
  `internal/discord/voice/sink/disgo_bridge.go` on the spike branch already
  does.
- `ChannelVoiceJoin(ctx, …)` (1 use) and `VoiceConnection.Dead` (3 uses) —
  both are the fork's lifecycle rework; disgo's `voice.Conn` has its own
  context-aware equivalents.
- `Session.DAVESessionCreate` — our injection point. disgo's is
  `voice.WithDaveSessionCreateFunc`, and it takes **the same type**.

## What carries over unchanged

The DAVE work does not get redone. `godave.SessionCreateFunc` is the type both
libraries accept, so `davesession.CreateFunc()` moves across as the same value.
Everything learned about it still applies, including the rule that cost a day:

> Never call into a `godave.Session` while holding the connection's lock. A
> session holds its own mutex while answering the protocol through
> `godave.Callbacks`, so anything that holds a connection lock and then asks
> the session a question deadlocks both.

That is a property of the interface, not of either library. disgo's `Conn` does
not have our `Cond`, but the rule still governs anything we write around it.

`voice-disgo-spike` is a working proof of the voice half: disgo's voice stack
driven by the discordgo gateway, with dave-go behind it, cross-compiling
cgo-free. Read it before rewriting that part.

## Where to pick this up

Everything is on `disgo-migration`, eighteen commits on top of `main`. `main`
is green and shippable; `go build ./...`, `go vet ./internal/... ./cmd/...`,
`go test ./...` and `go test -race ./...` are clean at every commit on this
branch.

"Clean at every commit" was also true of the commit that broke slash command
registration, which is worth remembering about what green means: no test
covered registration, so nothing had an opinion. The check that would have
caught it is the one that exists now.

One thing that does **not** travel: `dave-godave-seam-unsquashed` is a local-only
branch on the machine this was written on, holding the fourteen-commit history
of the DAVE work before it was squashed into `main`. Push it if that history is
worth keeping; otherwise it disappears with the machine, and the squashed
commit says everything that matters.

## Phase 2

It was planned as close to atomic: the session type threads through
`cmdadapter`'s context structs, into `reply`'s wire calls, into the root
handlers, and swapping any one alone does not compile.

That was true, and it was true for a removable reason. The seam was neutral in
its values and library-typed in its signatures: `Responder` had twelve methods
and every one took `(*discordgo.Session, *discordgo.InteractionCreate)`. The
two parameters were the library, spelled out twelve times. Commands could not
be freed of the session type because they were handed it.

So it slices after all, three steps, each one green:

**2a -- collapse the seam, still on discordgo. Done.** `Responder` answers one
interaction and is built per interaction, so it holds what answering needs
rather than taking it. `Invoker` carries guild, channel and caller, resolved
once by the handler. `SessionAPI` is what a command asks of the connection.
Arguments and component ids became data. The renderers moved to `reply`.
`cmdadapter` imports no Discord library, tests included.

A single receiver is the only shape both libraries satisfy: under discordgo a
reply needs the session and the event together, under disgo the event answers
for itself. This was phase 2's work, done in an order where the build never
broke.

**2b -- add the disgo implementation.** Siblings of the six packages that still
name discordgo: `reply` (responder, session API, renderers), `cmdsync`,
`cmdlogger`, `perm`, `voice/sink`, and the root handlers. Additive; nothing
existing breaks. `VOICE_BACKEND` comes across from `voice-disgo-spike` here.

**2c -- flip the bootstrap**, then delete `pkg/discordgo-fork-dev` and the
`replace` directive, which is the point of the whole exercise.

### Decisions taken

**`DISCORD_BACKEND` stays.** A startup switch between the two gateway stacks,
kept because the owner wants to see how it turns out and because removing it
later is deleting one implementation and one config branch -- which is only
cheap because the seam makes the two backends siblings behind one interface.
The cost is the interval, not the deletion: while both exist every change to
the reply surface is made twice, so the discordgo side should be frozen to
bugfixes once disgo works.

**`VOICE_BACKEND` stays too**, and is a different thing: `sink.Provider` is one
narrow interface with two live implementations in one process, so it is a real
A/B per guild rather than a restart. `DAVE_BACKEND` was the same idea and is
already gone -- a scaffold with a demolition date, demolished on schedule.

**No `Bot` interface above `internal/discord`.** The capabilities 2a had to
name -- reply, permissions, channel sends, guild counts, latency, registration,
voice state -- are the Bot's actual API. Naming them was the split; a second
seam over the first would have been ceremony.

## What phase 2 found on the way

- **Slash command registration was broken on this branch.** Phase 1 made
  `SlashProvider` declare the neutral `*SlashCommand` but left the Adapter's
  method returning `*discordgo.ApplicationCommand`. Nothing type-checked it:
  middleware unwraps to the Adapter, `cmdsync` asks the result for a
  `SlashProvider`, and an Adapter whose method returns a different type is not
  one. Every command resolved to no definition, and `SyncGuildCommands`
  reconciles -- an empty desired set **deletes everything the guild has**. The
  branch was green throughout because nothing covered registration. It does
  now, and compile-time assertions make the next drift a build failure.
- **`FindUserVoiceState` raced the session restart.** It read `b.dg` with no
  lock, on the command path, while a restart wrote that field. It was the one
  `VoiceAPI` method answering from the Bot's own session instead of
  delegating. Now in the voice service behind the getter that takes the lock.
- **`/commands status` told users commands are grouped "(ctx.Event.g., purge,
  core, translate)".** A scripted `e.` to `ctx.Event.` replacement reached
  inside a string literal during phase 1. It produced no compile error because
  the result was still a valid string. This one is live on `main`.
- **Dead weight:** `Bot.slashCmds` (declared, allocated, never used),
  `MessageApplicationCommandContext.Target`, `SlashInteractionContext.Args`,
  `Responder.EmbedColor()`, `CheckBotPermissions`. The last three are kept:
  this layer is shared verbatim with server-domme and the rule is to keep the
  two in step rather than trim to the local command set.
- **Three of the five context types are dead paths here.** Nothing implements
  `ReactionProvider` or `ContextMenuProvider`, and no command handles a
  `MessageContext`, so the @mention loop runs every command and every one
  declines. Kept for the same shared-layer reason, and worth confirming
  against server-domme before 2b ports them.
- **`WithGuildOnly` type-switched over two of the five contexts**, so a
  component interaction or context-menu command in a direct message reached a
  guild-only command. It asks the context now.

## What this work taught, worth not relearning

- **Never call into a `godave.Session` while holding the connection's lock.** A
  session holds its own mutex while answering the protocol through
  `godave.Callbacks`, so a lock held across that question deadlocks both. It
  cost a day, it is a property of the interface rather than of either library,
  and it will apply to disgo's voice stack too. See `docs/dave-todo.md`.
- **Scripted refactors fail in two specific ways here.** `*discordgo.MessageEmbed`
  is a prefix of `*discordgo.MessageEmbedField`, so a plain string replacement
  silently rewrites the wrong type; and a transform run over a whole tree will
  happily add a package's own import to a file inside it. Both were compile
  errors rather than silent behaviour changes, which is the only reason they
  were cheap.
- **Line endings are uniform now** -- all 244 tracked `.go` files are LF. The
  earlier warning about normalising CRLF before any string-matching edit no
  longer applies to Go source; check before trusting it again for other file
  types.
- **A green branch proves only what is tested.** Phase 1 changed an interface
  and left an implementation behind, and the type assertion between them
  failed silently for six commits because assertions do not fail to compile.
  Where a type switch or an assertion carries load, a compile-time
  `var _ Iface = (*T)(nil)` costs one line and turns the next drift into a
  build failure.
- **Check the branch before measuring.** A survey of `internal/discord` was run
  on `main` by mistake and disagreed with the recorded figure by 178
  references. Both numbers were right; only one of them was on this branch.
- `go test ./...` inside `pkg/discordgo-fork-dev` needs `-vet=off`: three
  pre-existing non-constant-format-string failures in `restapi.go` predate all
  of this.
- `internal/conventions` enforces 80-column comments and will fail the build on
  a long one. It catches them at `go test ./...`, not at `gofmt`.

## Open questions, answered

- **Is `internal/discord` the right seam, or should a `Bot` interface sit
  above it?** The cheaper answer was right, and the question was slightly
  mis-framed: the seam was already `cmdadapter`, it was just typed in the wire
  library. 2a fixed that and a second seam would have been ceremony.
- **Do disgo's slash registration and the `keshon/command` registry get
  along?** Yes, and orthogonally. disgo's `handler` package is its own router
  and entirely optional; registration is plain REST with a one-for-one match
  for the five calls `cmdsync` makes. Only the built type changes, to
  `discord.ApplicationCommandCreate` -- an interface whose three implementing
  types match `SlashCommandType`'s three-way split better than discordgo's one
  struct with a Type field. Two bonuses: `SetGuildCommands` is a bulk
  overwrite, one request instead of N with 25ms sleeps, which matters on a
  throttled link -- keep the fingerprint check to decide whether to call it;
  and `ApplicationID` is on the client, so `appID()`'s `User("@me")` fallback
  goes away.
- **No route from a `Conn` to its DAVE session in v0.19.6.** Confirmed absent,
  but the constraint it imposes is removable, and the spike's `pending` field
  is not the only option. `voice.WithConnCreateFunc` supplies the function the
  Manager calls per connection, and its signature takes `guildID` as a
  parameter -- so append a per-conn `voice.WithConnDaveSessionCreateFunc`
  whose hook closes over that guild, then call `voice.NewConn`. The session
  lands in the right slot by construction, and `CreateConn` no longer has to
  go through one type under one mutex. The create func runs under the
  Manager's `connsMu`, so the map needs its own lock -- but it only stores the
  pointer, never calls into the session, so the deadlock rule is not engaged.
- **disgo's bus factor.** Not answerable, but it can be priced: the expensive
  half -- DAVE, the one that cost a day -- is `godave.SessionCreateFunc` plus
  dave-go, which *both* libraries accept, so it survives disgo. The
  disgo-specific exposure is gateway, REST and voice transport: the part
  upstream discordgo also does.
- **`internal/discord` is a managed god object.** Its five roles are session
  lifecycle, handler wiring and dispatch, the command guard, the `VoiceAPI`
  facade, and holders for registration and audit logging. 2a named the
  capabilities, which is the split; what is left is for `Bot` to assemble them
  rather than be them. The `FindUserVoiceState` race was the visible symptom
  and is fixed.

## Still open

- Whether server-domme needs the three dead context types, `Args`,
  `EmbedColor()` and `CheckBotPermissions`. They are kept on the shared-layer
  rule; confirming would let 2b port less.
- disgo's heartbeat is an event (`events.HeartbeatAck`), not a field behind
  the session lock. That deletes `lastHeartbeatAck`, the 30-second probe
  timeout and the `session_lock_wedged` machinery -- all of which exist only
  because discordgo holds the session write lock across gateway reads. Worth
  confirming against a live run before deleting a watchdog that caught a real
  22-hour outage.
