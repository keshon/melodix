# Refactor plan (September 2026)

A working plan, not a reference document: it describes intended changes, so it
goes stale item by item as they land. Strike items through as they ship and
delete the file once the last phase is done — the commits keep the reasoning.

## Where this came from

The bot was ported to C# as an experiment (Discord.Net, .NET 10). Two things
came out of it:

1. Writing a behavioural spec of the Discord layer for the port surfaced real
   bugs in this codebase. All of them are in the Discord-facing half; the
   engine held up.
2. C# made some of this project's ownership rules hold by construction —
   immutable values from parsers, a playback run as an object with its own
   cancellation — where Go holds them with tests, checks and comments.

The plan fixes the first and carries the second over wherever Go allows it,
moving rules up the grades in [ownership.md](ownership.md): documented → tested
→ checked → compiler.

Every item follows [conventions.md](conventions.md#testing--verification): the
regression test is written first and watched failing against the bug before
the fix goes in.

---

## Phase 1 — user-visible bugs

Small commits, one bug each. Verified against the code at `779550a`.

| # | Bug | Where | Fix |
|---|---|---|---|
| 1.1 | The gateway watchdog fires once per session and exits, so under `ignore`, `restart-voice` or a grace count it never fires again. Both `.env.example` files also set `DISCORD_UNHEALTHY_MODE=ignore` and `INIT_SLASH_COMMANDS=true`, the opposite of the code defaults: copying the example turns session recovery off | `watchdog/ws_silence.go`, `config.go`, `.env.example`, `docker/.env.example` | Keep watching after a signal; align the examples and extend `config_doc_test.go` to check example values |
| 1.2 | `/history` page 1 shows the oldest 15 rows | `command/music/history/history.go` | Newest first; fix the footer |
| 1.3 | A full command lane replies "Bot is shutting down." | `discord/queue/queue.go`, `handlers_common.go` | `Submit` says why it refused |
| 1.4 | `/help flat` never lists subcommands: the assertion is made on the middleware wrapper | `discord/adapter/slash_render.go` | `command.Root(c)`, plus a test that the assertion matches |
| 1.5 | The disabled-group refusal points at `/commands status`, which does not exist | `middleware/group_check.go` | `/settings commands status` |
| 1.6 | Three permission lists disagree (README, running.md, runtime); the recommended set lacks Connect and Speak | `discord/perm/recommended.go`, `docs/running.md` | One list; the invite link is built from it |
| 1.7 | `DEVELOPER_ID` is parsed and never used; running.md promises developer-only commands | `config.go`, `perm/permissions.go` | Remove it |
| 1.8 | Starting playback edits the *previous* status message to the new track, which then stays stuck there | `voice/service.go`, `player.go`, `command/music/playback` | Folded into 2.E |
| 1.9 | A `/search` pick registers the ephemeral chooser as the status message: one person sees Now Playing, later edits fail | `command/music/search`, `voice/service.go` | Folded into 2.E |
| 1.10 | "Ephemeral" replies after a public defer are public, including the dispatcher's own error replies | `adapter/reply_methods.go`, `reply/responder.go`, `handlers_common.go` | Folded into 2.C |
| 1.11 | Component interactions bypass all middleware: no group check, no audit row | `discord/handlers.go`, `middleware/group_check.go` | Folded into 2.D |

**Suspected — prove with a failing test or a live check before fixing:**

- **Queue-end teardown vs `/play`.** If `/play` has enqueued but not yet called
  `PlayNext`, the finishing run's `stop(true, gen)` still sees its own
  generation, clears the queue with the new track in it and leaves voice.
- **yt-dlp live duration.** With no root duration, both yt-dlp parsers take the
  first fragment's, so a live stream can read as a five-second track and stop
  recovering (`ytdlp/link.go`, `ytdlp/pipe.go`).
- **SoundCloud metadata.** `scnative` overwrites Title and Duration
  unconditionally; a zero duration makes a track read as live.

Release after phase 1.

---

## Phase 2 — ownership by construction

### 2.A A parser returns what it learned as a value (rule 2: tested → compiler)

```go
type Opened struct {
	Reader      opus.Reader
	Cleanup     func()
	Passthrough bool
	Title       string
	Artist      string
	Duration    time.Duration // 0 = unknown
}

Open(track parsers.Track, seekSec float64) (Opened, error)
```

A parser handed a copy has nothing to write through. `RecoveryStream` stops
copying the Track to protect itself, and the suspected SoundCloud overwrite
goes with it. Five parser packages plus `stream`; no frozen identifier changes.

### 2.B A playback run is an object (rule 5)

```go
type run struct {
	stop      chan struct{}
	stopOnce  sync.Once
	done      chan struct{}
	track     parsers.Track
	announced string
	recorded  bool
}
```

`Player` holds `run *run` instead of `gen`, `stopPlayback`, `playbackDone` and
`stopOnce`. "Is this still my run?" becomes `p.run == r`, and a run's channels
cannot be confused with another run's because they are not on `Player` at all.
The queue-end race above is fixed here. Rule 5's tests stay as the net; its
text and the per-run-channel invariant in conventions.md are rewritten.

### 2.C Reply visibility follows the deferral (fixes 1.10)

The responder records whether the interaction was deferred publicly. An
ephemeral reply after a public deferral deletes the placeholder first, then
sends the followup. Cheap checks (voice channel, permissions) move before
`Defer()`. Checked: nothing in `internal/command` sends an ephemeral followup
around the responder. Needs a live check.

### 2.D Components go through the same pipeline as slash commands (fixes 1.11)

`handlers.go` runs the matched command with the component context, so group
check, guild-only and the audit logger apply; the dead branch in
`group_check.go` becomes live. Tested: a button in a disabled group is refused
and audited.

### 2.E The status message has one owner (fixes 1.8, 1.9)

Registering, dropping and ordering against `StatusPlaying` all live in the
voice service. Commands hand it the message they posted; the service decides.
A component interaction gets a public channel message, never the ephemeral
chooser.

---

## Phase 3 — keeping it fixed

- Linters, each only if it lands at zero findings: `exhaustive` for switches
  over statuses and modes; `errorlint`, which makes "never match error text"
  checkable.
- ownership.md: regrade rules 2 and 5; add reply visibility and the status
  message owner, each naming its check.
- The manual release matrix gains the scenarios the port found: a `/search`
  pick, `/next` while a status message is live, a second listener joining
  mid-track.

## Deliberately not taken from C#

- A DI container — "Go stays minimal".
- SQLite instead of the datastore — nothing wrong with it, and migrating
  history would cost more than it returns.
- Bulk-overwrite command sync — simpler, but the diff sync works and is tested.
