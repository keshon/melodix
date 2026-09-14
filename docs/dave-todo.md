# DAVE: where this stands and what to do next

Working document, written 14 September 2026, for picking this up in a fresh
session on another machine. It records what was measured, what is still a
guess, and which decisions are open. Delete it when the work lands.

Background: [issue #11](https://github.com/keshon/melodix/issues/11) — a bot
that joins a voice channel, plays nothing, and is disconnected a few seconds
later.

## Branches

**Nothing below is pushed.** All three branches are local only, with no
upstream set. Push them before switching machines.

| Branch | Base | Contents | Merge? |
| --- | --- | --- | --- |
| `voice-hold-frames` | `main` | One commit: stop sending unencrypted audio when there is no epoch. | Yes — ready |
| `dave-diagnostics` | `main` | `LOG_LEVEL` now reaches discordgo; close codes are named; the ignored MLS proposals opcode is visible. | Yes — it is what made every finding here readable |
| `voice-disgo-spike` | `dave-diagnostics` | The control experiment: disgo's voice stack plus dave-go, behind `VOICE_BACKEND`. Carries this document. | No — it exists to answer a question, and it has |

`voice-hold-frames` and the hold commit on `voice-disgo-spike` are the same
change, cherry-picked. Merging either is fine; merging both conflicts
trivially.

## What is actually wrong

Three links. Only the third was cheap to fix.

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

That fix converts the failure from *disconnected* into *silent, and saying so
once a second in the log*. It does not make audio play. Link 1 is the cure.

## What the runs showed

All on 14 September 2026, same guild, same track. The logs lived in `logs/`
and `tmp/` and are not committed.

| Run | Backend | What happened |
| --- | --- | --- |
| 08:34–08:40 | discordgo | Multi-account churn. Proposals arrived 11 times and were dropped every time; another member always committed, and a Welcome always followed within about a second. Six reject-and-re-Welcome cycles. Two gaps, 6 and 7 seconds, where the previous epoch's cipher was still in use. Both accounts heard audio. |
| 08:58–08:59 | disgo | Join, play, leave, rejoin. dave-go committed correctly on join and again on rejoin — and the gateway never answered the second commit. The pending epoch never activated and 796 frames went out on the stale sole-member epoch across 12 seconds. Silence. The run was stopped 3 seconds before dave-go's recovery watchdog would have fired. |
| 09:09–09:10 | disgo | Same script, longer wait. On rejoin the gateway rejected the bot's commit and sent a Welcome instead; dave-go rolled back and joined through it, in the same second, twice. Audio throughout. The recovery watchdog never engaged, so the 5s timeout on the spike branch is **not** what made this run work. |
| 09:16–09:17 | discordgo | The A/B, same script. Joined and was Welcomed. On leave the epoch was destroyed and nothing replaced it for 23 seconds — plaintext for all of it. On rejoin, Welcomed again and recovered in the same second. Audio was heard. |

Both stacks play music. They are not doing the same thing: disgo commits its
own group and holds a real epoch while alone, the fork has no epoch at all and
depends on someone else coming back.

## The options

### A. Put dave-go behind the fork — recommended

`voice.go`'s `onEvent` already dispatches every DAVE opcode: 21, 22, 24, 25,
27, 29, 30. What it lacks is a session that can answer them. Point those
handlers at a `godave.Session` backed by `thomas-vilte/dave-go` instead of the
hand-rolled `DAVESession`, and `dave.go`, `dave_crypto.go` and `mls/` — about
1,200 lines of RFC 9420 — can be deleted.

- Keeps discordgo, the gateway, all 50 call sites, the command layer.
- Gains the commit path, sole-member reset, a bounded retained-ratchet bridge
  across transitions, anti-replay, and external-sender validation — the fork's
  `HandleExternalSenderPackage` ignores its argument and returns nil.
- Pure Go. Verified on the spike branch, not assumed: `cmd/discord`
  cross-compiles for windows/amd64, windows/arm64 and darwin/arm64 with
  `CGO_ENABLED=0`, and `go list -deps` pulls in godave's interface package
  alone, never `golibdave`. The release workflow is untouched. Do not reach
  for `disgoorg/godave`'s own session instead: it links libdave over cgo, and
  `.github/workflows/release.yml` cross-compiles six targets with cgo off from
  Linux runners.
- Risk: dave-go is one person's library at v0.5.1 with little adoption, and so
  is the MLS library under it (`thomas-vilte/mls-go`). Read them before
  trusting them.

The adapter is mechanical — the opcode-to-callback mapping is one to one, and
`internal/discord/voice/sink/disgo_bridge.go` on the spike branch is a worked
example of the same wiring against the same interface.

### B. Move the voice path to disgo

What the spike branch already does, behind `VOICE_BACKEND`, with everything
above the audio path left on discordgo. It works. But it adds a second Discord
library to carry crypto the fork could carry itself, and disgo v0.19.6 offers
no route from a `Conn` to its DAVE session — `Conn.DAVE()` exists only on the
unreleased branch — so the session has to be caught from dave-go's hook, which
constrains how connections may be created. Option A buys the same crypto
without that.

### C. Finish the hand-rolled MLS

Ratchet tree, `UpdatePath` with HPKE to each subtree, transcript hashes,
confirmation tags, and generating Welcomes for joiners. Several thousand lines
of cryptography to write, test and own permanently, in order to arrive where
option A starts. Only worth it to avoid depending on dave-go.

### D. Ship the hold fix and stop

`voice-hold-frames` alone. The bot stops being disconnected and stops emitting
plaintext. It still cannot play into a channel where nobody commits for it, so
#11 gets silence instead of music. Defensible as a stopgap, not as an answer.

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
- **The fork's per-frame behaviour is unmeasured.** dave-go logs the epoch
  each frame was encrypted under, which is how the 796 stale frames were
  counted. The fork logs nothing equivalent, so its stale-epoch windows are
  known only from the gaps between lifecycle lines.

## Next, in order

1. Push all three branches.
2. Merge `dave-diagnostics` to main. It is small, correct on its own terms,
   and without it the next bug report is unreadable.
3. Merge `voice-hold-frames` to main. It stops the leak.
4. Ask the reporter on #11 to re-run on a build carrying the diagnostics and
   post the log. The `DAVE proposals … cannot commit` line and the named close
   code will say which of the two failures he has.
5. Decide between A and D.

## Loose ends on the spike branch

- `go mod tidy` has not been run, so the new modules sit in go.mod marked
  `// indirect` despite being imported directly.
- `daveRecoveryTimeout` in `internal/discord/voice/sink/disgo_bridge.go` is
  set to 5s against dave-go's 15s default. The reasoning is written at the
  constant. It has never been observed firing, so it is unvalidated.
- The fork has three pre-existing `go vet` failures in `restapi.go` and
  `restapi_test.go` (non-constant format strings) which make `go test ./...`
  fail to build inside `pkg/discordgo-fork-dev`. Unrelated to any of this; use
  `-vet=off` there. Melodix's own conventions checks skip the directory.
