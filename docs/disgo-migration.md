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

**Phase 1 is done.** Everything outside `internal/discord` holds zero
references to discordgo: 243 between them at the start, none now.

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

Everything is on `disgo-migration`, pushed, eight commits on top of `main`.
`main` is green and shippable; this branch has never broken the build or the
tests, and `go test ./...` and `go vet ./internal/... ./cmd/...` are clean at
every commit.

One thing that does **not** travel: `dave-godave-seam-unsquashed` is a local-only
branch on the machine this was written on, holding the fourteen-commit history
of the DAVE work before it was squashed into `main`. Push it if that history is
worth keeping; otherwise it disappears with the machine, and the squashed
commit says everything that matters.

## Phase 2, and why it is not phase 1 again

Phase 1 sliced cleanly because each package could be freed on its own and the
build stayed green between slices. **Phase 2 cannot be sliced that way.** The
session type threads through `cmdadapter`'s context structs, into `reply`'s wire
calls, into the root handlers; swapping any one of them alone does not compile.
It is closer to atomic than incremental, and planning it as a series of small
green steps will not survive contact.

The surface, measured on this branch:

| | refs | what it is |
| --- | --- | --- |
| `cmdadapter` | 173 | the context structs and the four converters |
| `reply` | 103 | every call that puts something on the wire |
| `perm` | 66 | mostly the permission-name table |
| root files | 60 | session bootstrap, handlers, the Bot type |
| `voice` | 19 | the status message and the sink |
| `cmdsync` | 10 | command registration |
| `cmdlogger` | 2 | |

### Suggested order

1. **Add disgo and stand up a second bootstrap** that connects, logs, and
   registers nothing. Worth doing first because it is the only part of phase 2
   that can be verified on its own: does it connect with the real token, and
   does it see events? Everything after this is a rewrite that only a live run
   can check.
2. **Port `cmdadapter`, `reply` and the root handlers together.** They cannot be
   separated. This is the large one and it is where the risk is: handlers become
   typed events under disgo's `events/` package rather than `AddHandler` with a
   func signature, and the context structs change what they hold.
3. **`cmdsync`, `perm`, `cmdlogger` and `voice` follow mechanically** once the
   session type has changed under them.
4. **Cut `cmd/discord` over behind a flag**, keeping the discordgo path until a
   live run confirms the disgo one -- the same discipline `DAVE_BACKEND` used,
   and for the same reason: a rewrite of this size is not something to make the
   default on the strength of a compile.
5. **Delete `pkg/discordgo-fork-dev` and the `replace` directive.** That is the
   point of the whole exercise.

### The open decision

Whether to do step 1 at all, or go straight at step 2. Step 1 costs a little
time and buys the only independent verification available in the whole phase.
It was put to the repo's owner and not yet answered.

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
- **Files in this repo have mixed line endings.** Anything that edits Go source
  by string matching has to normalise CRLF first and write back what it found,
  or every match fails and `gofmt` rewrites the whole file.
- **Check the branch before measuring.** A survey of `internal/discord` was run
  on `main` by mistake and disagreed with the recorded figure by 178
  references. Both numbers were right; only one of them was on this branch.
- `go test ./...` inside `pkg/discordgo-fork-dev` needs `-vet=off`: three
  pre-existing non-constant-format-string failures in `restapi.go` predate all
  of this.
- `internal/conventions` enforces 80-column comments and will fail the build on
  a long one. It catches them at `go test ./...`, not at `gofmt`.

## Open questions

- Whether `internal/discord` is the right seam or whether a `Bot` interface
  should sit above it, with neither library appearing in the signature. The
  cheaper answer is probably the right one; phase 1 already moved everything
  behind `cmdadapter`, which may be enough.
- Whether disgo's slash-command registration and the `keshon/command` registry
  get along, or whether `cmdsync` needs rethinking.
- disgo v0.19.6 offers no route from a `Conn` to its DAVE session --
  `Conn.DAVE()` exists only on the unreleased branch -- so the session has to be
  caught from dave-go's create hook, which constrains how connections may be
  created. `voice-disgo-spike` does this and the comment there explains it.
- disgo is one more third-party dependency with its own bus factor. It is far
  more active than upstream discordgo, which is the whole point, but it is not a
  guarantee.
