# DAVE: what was wrong, and what is left

Working document, 14 September 2026, kept for what it measured rather than
for what it planned. Its own instruction was to delete it once the work
landed; the measurements are still the only record of how any of this was
diagnosed, so it stays until somebody decides otherwise.

Two things in it have since moved and are corrected in place: the frame hold,
and the recovery timeout. Everything under [What is left](#what-is-left) is
still open.

Background: [issue #11](https://github.com/keshon/melodix/issues/11) — a bot
that joins a voice channel, plays nothing, and is disconnected a few seconds
later.

**Status: fixed on `dave-godave-seam`, ready to merge.** The bot holds its own
MLS epoch, alone or in company, and plays audio on an end-to-end encrypted
channel. Verified live with one account and with two. dave-go is now the only
implementation: the hand-rolled MLS is deleted and `DAVE_BACKEND` is gone with
it. What remains is under [What is left](#what-is-left).

## The branch

`dave-godave-seam` is one commit on top of `main`, squashed. It carries, in
order of how the work happened:

- `LOG_LEVEL` reaching the Discord library, and named close codes — the
  diagnostics without which none of the findings below were readable.
- Holding frames rather than sending them unencrypted, in both of the ways an
  epoch dies. That hold lived in the fork's send loop and did not survive the
  move to disgo, which owns the send loop now and never asks. It was restored
  at the one point on the pull-model path that is ours,
  `frameProvider.ProvideOpusFrame`, with the same predicate and a budget past
  which the track ends rather than holding forever. See
  `internal/discord/voice/voicesink/dave_hold_test.go`.
- `godave.Callbacks` over the connection's senders, receivers keyed by user,
  and the session moved behind `godave.Session`.
- `Session.DAVESessionCreate`, so the implementation is the caller's choice.
- dave-go wired in on melodix's side, then everything the live runs turned up:
  the codec assignment, the membership feed, the lock order, the per-channel
  reset.
- Deleting the hand-rolled MLS.

`voice-disgo-spike` still exists as the control experiment and is not for
merging. `dave-diagnostics` and `voice-hold-frames` were folded in here and
need no separate merge.

## What was wrong

Four links, all now closed.

**1. The implementation could not commit.** The fork's `mls/` package was a
joiner: it generates a key package, processes a Welcome, derives an exporter
secret. It holds no ratchet tree state, so it cannot produce a commit. The
proposals opcode (27) was received and dropped; an announced commit (29) was
answered with `invalid_commit_welcome` and a request to be re-Welcomed.

**2. So a bot alone in a channel never got an epoch.** `HandlePrepareEpoch`
tore the session down and waited for a Welcome that nobody was there to send.
Measured: 23 seconds in one run, ending only when a listener came back.

**3. With no epoch, the sender fell through to plaintext.** Audio leaving the
bot in the clear on a channel Discord marked end-to-end encrypted, to receivers
that discard it and a server entitled to close the connection for it. That is
what #11 looks like from outside. Fixed by holding the frames.

**4. And when somebody else committed, the bot kept encrypting under the epoch
they had just left.** The mirror image of link 3, and the one that hid longest,
because the frames were encrypted and well-formed — just undecryptable.
`ResetForReWelcome` dropped the exporter secret but left `active` and the
cipher in place, so `holdFrames` never engaged. This is the 6 and 7 second gaps
in the 08:34 run, recorded there as a measurement and actually a bug. Fixed; the discriminator is the exporter secret, which is set only by a
Welcome and dropped only when we deliberately ask for a new epoch.

Links 3 and 4 turned the failure from *disconnected* or *inaudible* into
*silent, and saying so*. Link 1 was the cure, and it came from replacing the
implementation rather than finishing it.

## What the swap turned up

Five more, none of which any test in this repo could have caught, because the
built-in session does not exercise the paths. All were found by running it.

**The codec was never assigned.** `AssignSsrcToCodec` was written as a no-op on
the built-in session — it treats everything as Opus and keeps no SSRC map — and
then never called at all, with a comment saying "not used" that made the
omission look deliberate. dave-go keys frame encryption by codec and refuses an
SSRC it was never told about: 616 `no codec assigned for ssrc` errors in one
run, and silence. disgo makes this call during UDP IP discovery; we make it
where the session is built, and again on opcode 2, because a reconnect
re-announces the SSRC.

**A failed encryption sent the plaintext.** Older than this work and worse than
the silence. The sender logged the error and carried on with the unencrypted
Opus still in the buffer, which the transport cipher then wrapped and sent. The
log shows both halves on consecutive lines: `DAVE encrypt error idx=600`
followed by `udp write ok idx=600 dave_active=true`. It is the same leak
`holdFrames` exists to close, reached through a door `holdFrames` cannot see —
the session reports Ready and encrypting fails anyway.

**Nothing told the session who was in the channel,** so dave-go declined to
commit: `ignoring add proposal for unexpected user`, then `skipping commit`. It
will not commit an add proposal naming somebody it has not been told about.

**The opcode is 11, plural, not 12.** This was the open question. Measured:

```
unknown voice operation, 11, {"user_ids":["365177820663513089"]}
```

"unknown" because the fork handled opcode 12, the singular one with an
`audio_ssrc`, which never arrived at all in any run. Opcode 11 also lands
*before* opcode 4 says the channel is encrypted, so the roster is kept on the
connection and replayed into each new session.

**Holding the connection lock while asking the session anything deadlocks.**
This one is a property of the interface, not of dave-go, and it is the most
important thing in this document to carry forward.

A DAVE session holds its own mutex while it works, and while holding it, it
answers the protocol through `godave.Callbacks` — which is the connection's
sender, which takes `v.Cond.L`. So a busy session is holding its mutex and
waiting for ours. Anything on our side that holds `v.Cond.L` and then calls
`Ready()`, `AddUser` or `AssignSsrcToCodec` closes the cycle.

The opus sender was computing `daveActive := dave != nil && dave.Ready()`
inside the lock. The log has the moment: frames encrypt normally to line 1515,
proposals arrive at 1516, dave-go commits and calls `SendMLSCommitWelcome` at
1530, and there is nothing after it. No frame was ever encrypted again, the
transport timed out, and the process would not shut down, because the playback
goroutine was wedged forever.

The built-in session never did this — its methods take and release per call and
never hold across a callback — so the inversion was harmless until a real
implementation arrived. **The rule is now: never call into a `godave.Session`
while holding `v.Cond.L`.** It is written at the top of the helpers in
`dave_inject.go`, pinned by a test that reproduces the lock order and wedges if
anyone reintroduces it, and it will apply just as much to disgo's voice stack,
which consumes the same interface.

**A reused connection kept the previous channel's state.** `VoiceConnection`s
are held per guild and reused across channels; `ChannelVoiceJoin` reset five
fields and left the roster and the SSRC map alone. A session told about absent
members would accept an add proposal naming them, and a stale SSRC would
decrypt somebody's audio under the wrong user's ratchet. Both get likelier with
more people in the channel. Fixed by resetting both on a channel change.

## What the runs showed

All on 14 September 2026. The logs lived in `logs/` and are not committed; note
that `melodix.json` is appended to, so a file can hold several runs.

| Run | Backend | What happened |
| --- | --- | --- |
| 08:34–08:40 | built-in | Multi-account churn. Proposals arrived 11 times and were dropped every time; another member always committed, and a Welcome always followed within about a second. Six reject-and-re-Welcome cycles. Two gaps, 6 and 7 seconds, on the previous epoch's cipher — since diagnosed as link 4 and fixed. Both accounts heard audio. |
| 08:58–08:59 | disgo spike | dave-go committed correctly on join and again on rejoin — and the gateway never answered the second commit. 796 frames went out on a stale sole-member epoch across 12 seconds. Silence. |
| 09:09–09:10 | disgo spike | Same script, longer wait. On rejoin the gateway rejected the bot's commit and sent a Welcome instead; dave-go rolled back and joined through it, in the same second, twice. Audio throughout. |
| 09:16–09:17 | built-in | The A/B. On leave the epoch was destroyed and nothing replaced it for 23 seconds — plaintext for all of it. On rejoin, Welcomed again and recovered in the same second. |
| first dave-go | dave-go | Silence. 616 `no codec assigned for ssrc` errors — one per frame. Everything else was correct: group created, Welcome processed, epoch activated with two senders. |
| second dave-go | dave-go | Join, play, leave, rejoin. **The leave worked**: on `prepare_epoch` dave-go created its own group and activated `epoch_id=0 sender_count=1`, and kept encrypting with the bot alone in the channel — the thing the built-in session can never do. The rejoin deadlocked on the lock order above. |
| third dave-go | dave-go | Single account, full cycle. Works. |
| fourth dave-go | dave-go | **Two accounts.** Two commits by the bot. `epoch_id=1 sender_count=2` when the first joined, `epoch_id=2 sender_count=3` when the second did. Zero DAVE errors, zero warnings, no `ignoring add proposal`, no held frames. |

`sender_count` tracking the size of the room is the single most useful signal
in these logs.

## The decision, and why

**Option A: put a `godave.Session` behind the fork.** Taken.

The seam is the **interface**, not dave-go. The voice stack depends on
`github.com/disgoorg/godave` — two files, no transitive dependencies, no cgo —
and melodix chooses the implementation at the composition root. `godave.Session`
turned out to be the vendored fork's opcode dispatch method for method, and
`Ready()` is documented upstream as exactly the frame hold that `cc2ab4b` had
arrived at independently.

That the interface was the seam is why the disgo migration did not touch any
of this: `godave.SessionCreateFunc` is the type both libraries accepted, so
dave-go crossed over as the same value. The fork is gone; this is not.

The abstraction is not ours: disgo defined it, disgo consumes it, and dave-go
asserts conformance at compile time. Changing crypto vendors later is a
constructor change in melodix, not surgery in the fork. It is also why **option
B — moving the voice path to disgo — is not blocked by any of this**: disgo's
voice stack takes the same `godave.SessionCreateFunc`, so the implementation
configured here is the same value, unchanged, that a disgo stack would take. If
that migration happens, the DAVE half is already done.

Option C, finishing the hand-rolled MLS, is dead: the measurement is mls-go at
28,416 lines of implementation and 25,982 of tests, to arrive where A starts.

Risk that remains: dave-go is one person's library at v0.5.1, and so is mls-go
under it. What inspection can say is that they are not toys — dave-go is 4,587
lines against 5,727 of tests, mls-go implements RFC 9420 and ships an interop
suite, both MIT. What inspection cannot fix is the bus factor, and that is what
the seam contains.

## What is left

1. **Merge to main.** The branch is a clean line from `main`, and merging it
   brings `dave-diagnostics` and `voice-hold-frames` with it.
2. **Close #11**, ideally after the reporter confirms on a build from this
   branch.
3. **Revisit option B on its own merits.** The question to ask is "do we want
   to own a voice stack?" — the fork's permanent cost is the 1,600-line
   rewritten `voice.go`, not DAVE. Nothing about it is urgent now.

Done since the first draft: the default was flipped and then the choice was
removed altogether. `dave.go`, `dave_crypto.go`, `mls/` and the `legacySession`
adapter are deleted — 1,491 lines of hand-rolled RFC 9420 and its scaffolding —
and melodix configures dave-go unconditionally at session bootstrap.

Keeping `builtin` as an escape hatch was considered and rejected: the hatch
opens onto the original bug, because falling back to it means silence on any
channel where nobody else commits. What replaced it is smaller and safer — a
connection whose `Session.DAVESessionCreate` is nil gets a session that is
never ready and refuses to encrypt, so an encrypted channel with no
implementation behind it holds its audio instead of sending it in the clear.
The seam itself stays: the fork still depends only on the interface, and
choosing a different implementation is still a constructor change in melodix.

## Still unknown

- **The unanswered commit in the 08:58 run.** dave-go's commit was correct and
  the gateway said nothing back. What did not arrive would have been a Welcome
  of about a kilobyte, which is the large-inbound-frame theory in the op-25
  comment in `voice.go`. Unproven either way, and not seen since.
- **Which failure #11 actually was.** The log shows a key package sent and no
  Welcome ever, which fits both "nobody was there to commit" and "the Welcome
  was lost". Both end with no epoch, and both are fixed, so this may never be
  answered.
- **Churn with several accounts.** Two accounts joining and leaving cleanly
  works. Rapid join/leave with several members in flight, and commit races
  where another client commits at the same moment the bot does, are unmeasured
  on this stack. dave-go has rollback for a rejected commit and was seen using
  it on the spike branch.
- **Receiving from several speakers.** `ssrcToUserID` is fed by opcode 5 and
  opcode 12, and opcode 12 never arrives — so a user's frames are decryptable
  only after they have sent a speaking event. Near-zero impact for a bot that
  mostly sends; it would matter if melodix ever consumed inbound audio.

## Loose ends

- `SetChannelID` is never called. In dave-go it only binds `channel_id` onto
  the session's log lines — no state depends on it — and the fork's
  `VoiceConnection` has no channel ID to pass, so it would need plumbing for a
  diagnostic gain. Worth doing if multi-guild logs ever need untangling.
- The opcode 12 handler is still there and has never fired in any run
  observed. Harmless, and cheap insurance if some guild or gateway version
  sends it.
- `daveRecoveryTimeout` is set to 5s against dave-go's 15s default, and has
  never been observed firing. `sink.DaveRecoveryTimeout` carries the reasoning.
- The vendored fork had three pre-existing `go vet` failures in `restapi.go`
  and `restapi_test.go` that needed `-vet=off` to test inside it. It has since
  been deleted, along with the CI step and the conventions skip that existed
  for it.
