# DAVE: where this stands and what to do next

Working document. Written 14 September 2026 and revised the same day after a
second session on another machine. It records what was measured, what is still
a guess, and which decisions are open. Delete it when the work lands.

Background: [issue #11](https://github.com/keshon/melodix/issues/11) — a bot
that joins a voice channel, plays nothing, and is disconnected a few seconds
later.

**The decision is made: option A, with the seam at `godave.Session`.** The
reasoning is under [The options](#the-options); the correction that produced it
is under [What changed in the second session](#what-changed-in-the-second-session).

## Branches

| Branch | Base | Contents | State |
| --- | --- | --- | --- |
| `voice-hold-frames` | `main` | Stop sending unencrypted audio when there is no epoch, and stop sending under an epoch the group has left. | Ready to merge |
| `dave-diagnostics` | `main` | `LOG_LEVEL` now reaches discordgo; close codes are named; the ignored MLS proposals opcode is visible. | Ready to merge — it is what made every finding here readable |
| `voice-disgo-spike` | `dave-diagnostics` | The control experiment: disgo's voice stack plus dave-go, behind `VOICE_BACKEND`. Carries this document. | Do not merge — it exists to answer a question, and it has |

`voice-hold-frames` and `dave-diagnostics` were lost before they were pushed,
and both were rebuilt from `origin/voice-disgo-spike`'s history: the two
diagnostics commits sit contiguously on top of `main` there, and the hold
commit cherry-picks onto `main` cleanly. Nothing was lost. All three now exist
locally; only the spike has an upstream.

## What is actually wrong

Four links. The last two were cheap to fix.

**1. The implementation cannot commit.** `pkg/discordgo-fork-dev/mls/` is a
joiner: it generates a key package, processes a Welcome, derives an exporter
secret. It holds no ratchet tree state at all, so it cannot produce a commit —
not for a joining member, and not for itself. The proposals opcode (27) is
received and dropped, and an announced commit (29) is answered with
`invalid_commit_welcome` plus a fresh key package, asking to be re-Welcomed.
Given what the MLS subset can do that is a deliberate and reasonable hack, not
an oversight.

**2. So a bot alone in a channel never gets an epoch.** `HandlePrepareEpoch`
tears the session down — `active` false, `frameCipher` nil, `exporterSecret`
nil — sends a key package, and waits for a Welcome. With nobody else present
nobody commits, and the Welcome never comes. Measured: 23 seconds in one run,
ending only when a listener came back.

**3. And with no epoch, the sender used to fall through to plaintext.** On a
channel Discord has marked end-to-end encrypted that is audio leaving the bot
in the clear, to receivers that discard it and a server entitled to close the
connection for it. That is what #11 looks like from outside. Fixed on
`voice-hold-frames`: frames are held instead.

**4. And when somebody else did commit, the bot kept encrypting under the
epoch they had just left.** The mirror image of link 3, and the one that hid
for longest, because the frames were encrypted and well-formed — just
undecryptable, so every listener dropped them. Silence that looks like working
audio from the sending end.

The path: opcode 29 announces the commit, `ResetForReWelcome` drops the
exporter secret and asks to be Welcomed into the new epoch, then
`execute_transition` arrives. `HandleExecuteTransition` found no pending key,
found `senderKey` still set, and returned early with `active` still true —
leaving a cipher derived from the previous epoch's secret in place. Unlike
`HandlePrepareEpoch`, nothing cleared it, so `holdFrames` never engaged. This
is the 6 and 7 second gaps in the 08:34 run, which were recorded there as a
measurement and are a bug. Fixed on `voice-hold-frames`: the discriminator is
the exporter secret, which is set only by a Welcome and dropped only when we
deliberately ask for a new epoch, so a sender key that outlives it always
belongs to an epoch nobody is in.

Links 3 and 4 convert the failure from *disconnected* or *inaudible* into
*silent, and saying so in the log*. Neither makes audio play. Link 1 is the
cure.

## What the runs showed

All on 14 September 2026, same guild, same track. The logs lived in `logs/`
and `tmp/` and are not committed.

| Run | Backend | What happened |
| --- | --- | --- |
| 08:34–08:40 | discordgo | Multi-account churn. Proposals arrived 11 times and were dropped every time; another member always committed, and a Welcome always followed within about a second. Six reject-and-re-Welcome cycles. Two gaps, 6 and 7 seconds, where the previous epoch's cipher was still in use — since diagnosed as link 4 and fixed. Both accounts heard audio. |
| 08:58–08:59 | disgo | Join, play, leave, rejoin. dave-go committed correctly on join and again on rejoin — and the gateway never answered the second commit. The pending epoch never activated and 796 frames went out on the stale sole-member epoch across 12 seconds. Silence. The run was stopped 3 seconds before dave-go's recovery watchdog would have fired. |
| 09:09–09:10 | disgo | Same script, longer wait. On rejoin the gateway rejected the bot's commit and sent a Welcome instead; dave-go rolled back and joined through it, in the same second, twice. Audio throughout. The recovery watchdog never engaged, so the 5s timeout on the spike branch is **not** what made this run work. |
| 09:16–09:17 | discordgo | The A/B, same script. Joined and was Welcomed. On leave the epoch was destroyed and nothing replaced it for 23 seconds — plaintext for all of it. On rejoin, Welcomed again and recovered in the same second. Audio was heard. |

Both stacks play music. They are not doing the same thing: disgo commits its
own group and holds a real epoch while alone, the fork has no epoch at all and
depends on someone else coming back.

## What changed in the second session

Three measurements moved the decision. All of them contradict something the
first draft of this document asserted.

**`disgoorg/godave` is an interface, not a cgo implementation.** The first
draft said "do not reach for `disgoorg/godave`'s own session: it links libdave
over cgo". That is true of the *implementation* and false of the *package*.
`godave` v0.2.0 is two files, imports only `log/slog`, and contains no cgo at
all. It defines `Session` and `Callbacks` and nothing else. The cgo lives in a
separate libdave-backed implementation that nothing here has to use. Reading
the warning as covering the whole module is what kept the best seam off the
table for a session — and it is also why "migrate to disgo" was ruled out
early, since disgo's DAVE support is exactly this interface with an injected
implementation.

**`godave.Session` is already the fork's opcode dispatch, method for method.**

| `godave.Session` | voice gateway |
| --- | --- |
| `OnSelectProtocolAck` | op 4 |
| `OnDavePrepareTransition` | op 21 |
| `OnDaveExecuteTransition` | op 22 |
| `OnDavePrepareEpoch` | op 24 |
| `OnDaveMLSExternalSenderPackage` | op 25 |
| `OnDaveMLSProposals` | op 27 |
| `OnDaveMLSPrepareCommitTransition` | op 29 |
| `OnDaveMLSWelcome` | op 30 |

`godave.Callbacks` is the send side and is the fork's four send functions
unchanged. `Session.Ready()` is `holdFrames`: its own documentation says
"AudioSenders should hold frames while Ready returns false instead of sending
them unencrypted", which is the fix on `voice-hold-frames`, arrived at
independently. The surface the fork actually uses is 14 methods across 18
references in `voice.go` — the first draft's "all 50 call sites" was counting
something else and overstated the work.

**The fork's divergence is a voice stack, not a DAVE patch.** Against upstream
`v0.29.0`, ignoring line endings (a first pass reported 38,664 changed lines,
which was CRLF inflation and wrong):

| File | Changed / total |
| --- | --- |
| `voice.go` | 1604 / 1598 — effectively a full rewrite |
| `dave.go`, `dave_crypto.go`, `mls/` | 1190 new |
| `wsapi.go` | 191 / 998 |
| `structs.go` | 46 / 3096 |
| `event.go`, `restapi.go` | 9 |

About 2,200 lines. Option A removes the 1,190 and leaves the rewritten
`voice.go` in place, which is the part that has to be maintained forever. That
is the fork's real cost, and it is not DAVE.

## The options

### A. Put a `godave.Session` behind the fork — chosen

`voice.go`'s `onEvent` and `handleDAVEBinary` already dispatch every DAVE
opcode. What they lack is a session that can answer them. Point those handlers
at the `godave.Session` interface, implement `godave.Callbacks` with the four
send functions that already exist, gate the send path on `Ready()` instead of
`holdFrames`, and `dave.go`, `dave_crypto.go` and `mls/` — about 1,190 lines of
RFC 9420 — can be deleted.

The seam goes at the **interface**, not at dave-go:

```
internal/discord/voice          chooses and constructs the implementation
      dave-go session (pure Go)   <- today
      godave noop                 <- channels with no E2EE; replaces `dave == nil`
      libdave-backed (cgo)        <- exists; never for our release matrix
            |
            |  injected as godave.Session
            v
pkg/discordgo-fork-dev          websocket, opcode decode, Callbacks,
                                SSRC/user map, holding frames on !Ready()
      depends on: disgoorg/godave   (2 files, no deps, no cgo)
```

- The fork gains exactly one dependency, and it is an interface with no
  transitive weight. mls-go's 28,000 lines are melodix's dependency, declared
  at the composition root where implementation choices belong.
- The abstraction is not ours. disgo defined it, disgo consumes it, and
  dave-go asserts conformance at compile time (`var _ godave.Session =
  (*Session)(nil)`). Changing crypto vendors later is a constructor change in
  melodix, not surgery in the fork.
- Gains the commit path, sole-member reset, a bounded retained-ratchet bridge
  across transitions, anti-replay, and external-sender validation — the fork's
  `HandleExternalSenderPackage` ignores its argument and returns nil.
- Pure Go. Verified on the spike branch, not assumed: `cmd/discord`
  cross-compiles for windows/amd64, windows/arm64 and darwin/arm64 with
  `CGO_ENABLED=0`, and `go list -deps` pulls in godave's interface package
  alone, never `golibdave`. The release workflow is untouched.
- Risk: dave-go is one person's library at v0.5.1, and so is the MLS library
  under it. What inspection can say is that they are not toys — dave-go is
  4,587 lines against 5,727 lines of tests; mls-go is 28,416 against 25,982,
  implements RFC 9420, ships an interop suite, and is MIT. What inspection
  cannot fix is the bus factor, and that is exactly what the seam contains: if
  either library goes bad, the fork never notices.

Two pieces are more than mechanical:

- **`AddUser`/`RemoveUser` is new wiring.** The fork never feeds membership to
  DAVE at all. disgo drives it from `CLIENTS_CONNECT` (op 11, plural) and
  `CLIENT_DISCONNECT` (op 13); the fork handles op 12 `CLIENT CONNECT`
  (singular) and op 13. Which opcodes this bot actually receives has to be
  checked against live traffic, not read off.
- **`Encrypt`/`Decrypt` change shape** — caller-allocated buffers sized by
  `MaxEncryptedFrameSize`, rather than returning a fresh `[]byte`. It touches
  the hot path, and it removes a per-frame allocation.

Everything else exists in one-to-one form, and
`internal/discord/voice/sink/disgo_bridge.go` on the spike branch is a worked
example against this same interface.

### B. Move the voice path to disgo — later, on its own merits

What the spike branch already does, behind `VOICE_BACKEND`. The first draft
argued against it on the grounds that it "adds a second Discord library to
carry crypto the fork could carry itself". With the divergence measured, the
honest version is the opposite: the fork's permanent cost is the 1,600-line
rewritten `voice.go`, and disgo maintains that for you — now without cgo, since
the DAVE implementation is injected.

What still defers it is sequencing, not merit. 53 files import discordgo
(`internal/discord/reply/interaction.go` at 73 references,
`internal/middleware/permissions.go` at 55, plus every command, `cmdsync` and
`cmdadapter`). Migrating that now means rewriting the command surface while the
voice path is the thing under investigation and #11 is still unreproduced —
two variables at once during a bug hunt.

Nothing is wasted by waiting, because A is on the path to B: after A the fork's
`voice.go` talks to `godave.Session`, and so does disgo's `voice.Conn`. The
adapter transfers unchanged, and the migration that remains is commands and
REST — mechanical and testable, and evaluable incrementally behind
`VOICE_BACKEND`.

The question to ask afterwards is "do we want to own a voice stack?", not "how
do we get DAVE?".

### C. Finish the hand-rolled MLS — dead

Ratchet tree, `UpdatePath` with HPKE to each subtree, transcript hashes,
confirmation tags, and generating Welcomes for joiners. The first draft
guessed "several thousand lines". The measurement is mls-go: 28,416 lines of
implementation and 25,982 of tests, to arrive where option A starts. Only
worth it to avoid depending on dave-go, and not worth it for that.

### D. Ship the hold fixes and stop

`voice-hold-frames` alone. The bot stops being disconnected, stops emitting
plaintext, and stops emitting audio nobody can decrypt. It still cannot play
into a channel where nobody commits for it, so #11 gets silence instead of
music. This is the interim while A is built, not the answer.

## Backend selection is the house pattern — with one difference

melodix already swaps implementations behind a narrow interface in three
places: `parsers.Streamer` (ytnative, kkdai, ytdlp, scnative, ffmpeg),
`sink.AudioSink`/`sink.Provider` (discord, speaker), and the voice stack behind
`VOICE_BACKEND`. DAVE is the fourth, and it should look like the third.

It must **not** look like the first. Parsers have runtime ordered fallback —
`PreferParser` reorders the chain, `AvailableParsers` rides along on each
track, and the player falls forward when a parser opens and then dies. DAVE
cannot work that way: MLS group state is shared with the server and the other
members, so a failure is not retryable with a different implementation, it is a
rejoin. Parser keys are also frozen, persisted and offered as slash-command
choices, and a DAVE backend must not become a user-facing choice — there is no
reason for anyone to pick one, and one of them is cgo.

One choice, made once, at boot.

## Still unknown

- **The unanswered commit in the 08:58 run.** dave-go's commit was correct and
  the gateway said nothing back — no acceptance, no rejection. What did not
  arrive would have been a Welcome of about a kilobyte, which is the
  large-inbound-frame theory already written into the op-25 comment in
  `voice.go`. Unproven either way. Timing does not explain it: the failing
  rejoin was 4 seconds after leaving and a working one was 5.
- **Which failure #11 actually is.** That log shows a key package sent and no
  Welcome ever, which fits both "nobody was there to commit" and "the Welcome
  was lost on the way". Both end with no epoch. The build in question also
  predates `dave-diagnostics`, so the opcode trace that separates them was
  being discarded inside discordgo before anything could write it down.
- **Which client-connect opcode this bot receives.** Blocks the
  `AddUser`/`RemoveUser` wiring above; answerable from one run with the
  diagnostics build.

## Next, in order

1. Push `dave-diagnostics` and `voice-hold-frames`. Only the spike has an
   upstream, and the other two have been lost once already.
2. Merge `dave-diagnostics` to main. It is small, correct on its own terms,
   and without it the next bug report is unreadable.
3. Merge `voice-hold-frames` to main. It stops both leaks.
4. Ask the reporter on #11 to re-run on a build carrying the diagnostics and
   post the log. The `DAVE proposals … cannot commit` line and the named close
   code will say which of the two failures he has, and the same log answers the
   client-connect opcode question.
5. Build the adapter (option A), implementation behind `godave.Session` first
   and deletion of `mls/` last, so the bot works at every commit.

## Loose ends on the spike branch

- `go mod tidy` has not been run, so the new modules sit in go.mod marked
  `// indirect` despite being imported directly.
- The spike's modules are not in every machine's module cache — `go mod
  download` is needed before it builds on a fresh checkout.
- `daveRecoveryTimeout` in `internal/discord/voice/sink/disgo_bridge.go` is
  set to 5s against dave-go's 15s default. The reasoning is written at the
  constant. It has never been observed firing, so it is unvalidated.
- The fork has three pre-existing `go vet` failures in `restapi.go` and
  `restapi_test.go` (non-constant format strings) which make `go test ./...`
  fail to build inside `pkg/discordgo-fork-dev`. Unrelated to any of this; use
  `-vet=off` there. Melodix's own conventions checks skip the directory.
