// Package command defines Discord slash commands, the [DiscordAdapter], and interaction context
// types. Commands register on [github.com/keshon/commandkit.DefaultRegistry] (see cmd/discord).
//
// Subpackages mirror the CLI tree:
//   - [github.com/keshon/melodix/internal/discord/command/core] — help, maintenance, …
//   - [github.com/keshon/melodix/internal/discord/command/music] — /music
//
// Melodix uses two command stacks that share the commandkit contract but not registries: this
// package plus DefaultRegistry for Discord, and [github.com/keshon/melodix/internal/cli/command]
// with a dedicated [github.com/keshon/commandkit.Registry] for the terminal REPL. That separation
// avoids collisions between Discord-global registration and the CLI process.
package command
