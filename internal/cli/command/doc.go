// Package command provides commandkit adapters, [Data], and shared helpers ([SplitQuoted], [AllUintStringTokens])
// for the terminal REPL.
//
// Subpackages mirror [internal/discord/command]:
//   - [github.com/keshon/melodix/internal/cli/command/music] — play, history, queue, …
//   - [github.com/keshon/melodix/internal/cli/command/core] — help
//
// Melodix intentionally uses two command stacks:
//   - This tree + a dedicated commandkit.Registry for CLI (see [github.com/keshon/melodix/internal/cli.RegisterCLICommands]).
//   - internal/discord/command + commandkit.DefaultRegistry for Discord slash commands and middleware.
//
// They share the commandkit contract but do not share registries, avoiding collisions between
// Discord-global registration and the CLI process.
//
// [Data] carries [github.com/keshon/melodix/internal/musicapp.Music] as the application facade for
// history and enqueue (same backing store as [Data.Store]).
package command
