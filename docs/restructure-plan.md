# Restructure plan (September 2026)

A working plan: strike items through as they land, delete the file when the
last step ships.

**Goal.** CLI and Discord become equal frontends over one shared layer. The
CLI mirrors Discord's music commands. The same shape carries over to
server-domme.

## Target

```
cmd/       cli/  discord/  readme/          wiring only
internal/
  music/                                    shared app layer
  readme/                                   renders both frontends
  cli/      cli.go status.go                REPL, Command/Context/Registry, status printer
            command/catalog/ play/ next/ stop/ queue/ search/ history/ help/
  discord/  (transport as today)
            command/catalog/ core/… music/… settings/
            middleware/
  config/ storage/ applog/ conventions/
```

## Rules

- `music` never starts playback and never renders. Each frontend starts
  playback itself: `PlayNext` in the CLI, `PlayNextAndAnnounce` in Discord,
  which detaches the old status message before starting.
- `music.New(cfg, store, log, Hooks{NewSink, Watch, OnFailed})` is built by
  each frontend (Discord inside `NewBot`). `Watch` runs once per player, so
  `PlayerStatus` keeps exactly one consumer.
- `music` API: `Player(scope)`, `Add`, `Search`, history rows, `StopAll`,
  `InvalidateSinks`. No wrappers for skip, stop or queue.
- One package per command, on both sides; the type is named `Command`.
  `catalog.Register` is the single command list per frontend.
- Formatters are per frontend. The `common` package is dissolved.
- CLI history: open storage whenever possible, and run without history on
  `datastore.ErrLocked`. The CLI's scope is `"cli"`.

## Steps

1. ~~**Adapter boundary as an allowlist.** `TestDiscordStaysBehindTheAdapter`
   lists the transport packages instead of exempting the `internal/discord/`
   prefix.~~
2. ~~**Moves only.** `command` and `middleware` under `internal/discord`,
   `musicwire` → `internal/music/layers.go`; then
   `[enforced: frontend-boundary]` (`TestFrontendsStayApart`).~~
3. ~~**`music.Service`.** Players, sinks, resolver, recorder and parser
   loggers moved in; `voice.Service` is plugged in as `Hooks`; `cmd/cli` uses
   it.~~
4. ~~**`music.Add` / `Search` / history.** `ParseInput`, history ids, source →
   searcher, timeline and counts moved in; `/play`, `/search`, `/history`
   shrank onto them.~~
5. ~~**Discord catalog.** `catalog.Register` (with the slash-refs test),
   types named `Command`, `common` → `tracklist` plus `reply.ClampEmbedText`.~~
6. ~~**CLI frontend.** `cli.Command`/`Context`/`Registry`/`Run`, status and
   failure printers; about, help, play, search, next, queue, stop, history
   under `cli/command`, mirroring the bot's layout; `cli/command/catalog`.~~
7. ~~**`cmd/readme`.** `readme.Generate` renders both catalogs through one
   `section`; the template has a Discord and a Terminal list; `-readme` is
   gone from the bot and `build-readme.bat` runs `./cmd/readme`.~~
8. **Docs.**
   - Update `architecture.md` (package map, CLI section), the `ownership.md`
     paths, and the `PlayerStatus` invariant wording.
   - Delete this file.

Each step: `go test -race ./...`, `golangci-lint run` and the conventions
tests stay green. Steps 3–6 also run the manual smoke checklist.
