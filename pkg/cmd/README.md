# Universal command layer (`pkg/cmd`)

This package is the **single** command orchestration layer for the project. It defines a transport-agnostic command: name, description, and `Run(ctx, invocation)`. The same `pkg/cmd` can be reused across:

- **Discord bots** — slash commands, context menus, message handlers
- **CLI apps** — `os.Args`-style dispatch with flags and subcommands
- **Telegram bots** — handlers that map to the same logical commands
- **Web/API** — HTTP routes or RPC that map to commands

## What lives here

| Concept   | In `pkg/cmd`                | In adapters (Discord / CLI / Telegram / HTTP)     |
|----------|-----------------------------|----------------------------------------------------|
| Identity | `Name()`, `Description()`   | + Group, Category, Permissions, Help, etc.         |
| Execution| `Run(ctx, *Invocation)`     | Build `Invocation` from event/args/request         |
| Registry | `DefaultRegistry`, `Register`, `Get`, `GetAll` | Dispatch (slash, CLI, router)     |
| Middleware | `Middleware`, `Apply`, `Wrap` | Same pattern; adapter adds transport-specific mw  |
| Registration | —                        | Slash definition, `Flags()`, subcommands, routes  |

So: the **entity** (name, description, run) is shared; **how** it’s registered and how the context is built is per application.

## Using from a Discord bot (this project)

- **`internal/command`** holds the Discord adapter: context types (`SlashInteractionContext`, etc.), providers (`SlashProvider`, `ContextMenuProvider`, …), and `DiscordCommand` with `Run(ctx interface{})`.
- **`DiscordAdapter`** implements `cmd.Command` and delegates to a `DiscordCommand`; it’s registered with `command.RegisterCommand(discordCmd, middlewares...)`, which uses `cmd.DefaultRegistry` and `cmd.Apply`.
- The bot gets commands from `cmd.DefaultRegistry.GetAll()` / `Get(name)`, builds `&cmd.Invocation{Data: discordCtx}` and calls `c.Run(ctx, inv)`. For slash/context menu registration it uses `cmd.Root(c)` and type-asserts to `SlashProvider` / `ContextMenuProvider`.

## Using from a CLI or another app

- Implement `cmd.Command` (or an adapter that wraps your existing command type).
- Register with `cmd.DefaultRegistry.Register(cmd)` (or your own `cmd.Registry`).
- Dispatch: resolve the command by name, build `Invocation` (e.g. from `os.Args` or `flag.FlagSet`), call `command.Run(ctx, inv)`.

## Wrapping and middleware

- **`cmd.Wrap(c, run)`** returns a command that runs `run` instead of `c.Run`; used by middleware.
- **`cmd.Root(c)`** unwraps through `Unwrappable` to get the underlying command (e.g. to type-assert to `SlashProvider` or adapter-specific interfaces).
- Adapters apply middlewares with `cmd.Apply(adapter, mw1, mw2, ...)` before registering.

## Summary

- **One package** for command orchestration: `pkg/cmd`.
- **Adapters** (Discord, CLI, Telegram, HTTP) define their own context types and registration; they implement or wrap `cmd.Command` and use `cmd.Registry` + `cmd.Middleware`.
