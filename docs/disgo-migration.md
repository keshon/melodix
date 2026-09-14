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

98 distinct discordgo symbols, 489 references, 53 files.

| Package | refs | files | what it is |
| --- | --- | --- | --- |
| `internal/discord` | 246 | 29 | the adapter layer — where this *should* be |
| `internal/command` | 171 | 18 | commands reaching past the adapter |
| `internal/middleware` | 64 | 3 | permissions and logging |
| `internal/readme` | 8 | 1 | command documentation generation |

The heaviest types, in order: `MessageEmbed` (107), `Session` (76),
`InteractionCreate` (44), `ApplicationCommand` (33), the interaction response
family (~30), `VoiceConnection` (8).

So this is really three jobs — embeds, slash commands and interactions, and the
session — and one of them is most of it.

The disgo equivalents exist and are close in shape: `discord.Embed`,
`discord.SlashCommandCreate`, `discord.ApplicationCommandInteraction`,
`discord.InteractionResponse`. Handlers are typed events under `events/` rather
than `AddHandler` with a func signature, which is a mechanical but
across-the-board change.

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

## Plan

**Phase 1 — stop the leak.** Move the 243 references outside `internal/discord`
(commands, middleware, readme) behind melodix's own types. `cmdadapter` and
`reply` already exist for this; they just do not cover everything. No command
should import discordgo.

This is worth doing on its own merits and can be verified against the current
library, one package at a time, with `main` shippable throughout. It also turns
phase 2 from a 53-file change into a one-package change.

**Phase 2 — swap the implementation.** With the surface contained, rewrite
`internal/discord` against disgo: session bootstrap, handlers as typed events,
embeds, interactions, command registration.

**Phase 3 — voice.** Behind `VOICE_BACKEND`, the pattern the spike established,
so both stacks can run in one build and be compared on a live channel rather
than across two. Fold in the spike's `disgo_bridge.go`.

**Phase 4 — delete the fork.** `pkg/discordgo-fork-dev` and the `replace`
directive go. This is the point of the whole exercise.

Phases 1 and 2 are ordinary refactoring. Phase 3 is the one that needs live
testing, and the checklist for that is in `docs/dave-todo.md` — the same
scenarios that flushed out the five bugs above: join, play, second account
joins, one leaves, bot left alone, rejoin. Watch `sender_count` track the room.

## Open questions

- Whether `internal/discord` is the right seam or whether phase 1 should push
  further, to a `Bot` interface that neither library appears behind. The
  cheaper answer is probably the right one; decide when phase 1 is underway.
- Whether disgo's slash-command registration and `keshon/command` registry get
  along, or whether `cmdsync` needs rethinking.
- disgo v0.19.6 offers no route from a `Conn` to its DAVE session —
  `Conn.DAVE()` exists only on the unreleased branch — so the session has to be
  caught from dave-go's create hook, which constrains how connections may be
  created. The spike does this and the comment there explains it.
- disgo is one more third-party dependency with its own bus factor. It is far
  more active than upstream discordgo, which is the whole point, but it is not
  a guarantee.
