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
3. **`music.Service`.**
   - Move in player construction, the registry, the sink providers, the
     recorder and the parser `SetLogger` calls.
   - `voice.Service` keeps status messages, voice state and rendering.
   - `cmd/cli` uses `music.Service`, which removes the duplicated config
     parsing.
4. **`music.Add` / `Search` / history.**
   - Move in `play_input.go`, the history-id lookup, source → `Searcher`
     selection, and timeline/count rows.
   - The Discord commands shrink to use them.
5. **Discord catalog.**
   - `registerCommands` → `discord/command/catalog`.
   - Rename types to `Command`.
   - Dissolve `common`: formatters go to a package named for what they do.
6. **CLI frontend.**
   - Add `cli.Command` (`Name`, `Aliases`, `Category`, `Usage`, `Run`),
     `Context`, `Registry` and the REPL.
   - Commands: play, next, stop, queue, search, history, help.
   - Add `cli/command/catalog`.
7. **`cmd/readme`.**
   - Split `readme` into its shared core plus `discord.go` and `cli.go`.
   - The template gets `{{ .DiscordCommands }}` and `{{ .CLICommands }}`.
   - Both sections use the same category headings and order
     (`config.CategoryWeights`, `plainCategory`), taken from each command's
     `Category()`.
   - Drop `-readme` from `cmd/discord`; `build-readme.bat` becomes
     `go run ./cmd/readme`.
8. **Docs.**
   - Update `architecture.md` (package map, CLI section), the `ownership.md`
     paths, and the `PlayerStatus` invariant wording.
   - Delete this file.

Each step: `go test -race ./...`, `golangci-lint run` and the conventions
tests stay green. Steps 3–6 also run the manual smoke checklist.
