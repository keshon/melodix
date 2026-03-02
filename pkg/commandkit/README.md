Readme хорошо написан — структура чёткая, примеры на месте. Вот что я бы поменял:
# commandkit

A transport-agnostic command execution core for Go.

Defines a minimal contract for commands — identity + execution — and provides a registry, middleware, and safe adapter unwrapping. Transport concerns (CLI flags, HTTP routing, Discord slash definitions) belong in adapters, not here.

---

## When to use this

When you want the same command logic to run across multiple transports — CLI, Discord/Telegram bots, HTTP APIs, background workers — without coupling it to any of them.

---

## Core concepts

### Command

```go
type Command interface {
    Name() string
    Description() string
    Run(ctx context.Context, inv *Invocation) error
}
```

Permissions, flags, subcommands, and routing are adapter concerns.

### Invocation

```go
type Invocation struct {
    Args []string
    Data interface{}
}
```

`Data` is an opaque adapter-defined payload (event, request, session). Type safety is enforced at the adapter boundary.

### Registry

Stores commands by name. Does not dispatch or execute — adapters decide how and when commands are invoked.

```go
command.DefaultRegistry.Register(cmd)
command.DefaultRegistry.Get("ping")
command.DefaultRegistry.GetAll()
```

A global `DefaultRegistry` is provided for convenience; inject your own `Registry` where isolation matters (e.g. tests).

### Middleware

```go
type Middleware func(Command) Command
```

Logging, metrics, permission checks, panic recovery — anything cross-cutting. Stays transport-agnostic.

### Wrapping and unwrapping

`Wrap` replaces `Run` while preserving identity. `Root` unwraps a middleware chain back to the original command — useful when adapters need to type-assert to transport-specific interfaces (e.g. `SlashProvider`).

---

## Usage

```go
// Define
type PingCommand struct{}
func (PingCommand) Name() string        { return "ping" }
func (PingCommand) Description() string { return "Health check" }
func (PingCommand) Run(ctx context.Context, inv *commandkit.Invocation) error {
    return nil
}

// Register
commandkit.DefaultRegistry.Register(PingCommand{})

// Execute (from an adapter)
cmd := registry.Get("ping")
err := cmd.Run(ctx, &commandkit.Invocation{Args: args, Data: adapterCtx})
```

---

## What lives where

| Concern | commandkit | Adapter |
|---|---|---|
| Identity | `Name()`, `Description()` | Category, permissions, help |
| Execution | `Run(ctx, *Invocation)` | Build `Invocation` from event/request |
| Registry | `Register`, `Get`, `GetAll` | Dispatch logic |
| Middleware | `Middleware`, `Apply`, `Wrap` | Transport-specific middleware |
| Registration | — | Slash definitions, flags, routes |
