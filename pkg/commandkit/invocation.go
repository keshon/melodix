// Package commandkit provides a transport-agnostic command core. A command has
// a name, description, and Run(ctx, invocation). Registration and dispatch
// (Discord slash, CLI, HTTP) are defined by adapters that use this package.
package commandkit

import "context"

// Invocation carries the minimal input a command runner passes: arguments and
// an opaque payload. Adapters set Data to their context (e.g. *discordgo.Session
// and event, or *flag.FlagSet and CLI context).
type Invocation struct {
	Args []string
	Data interface{}
}

// Command is the core contract: identity and execution. Permissions, flags,
// subcommands, and transport-specific registration belong in adapters.
type Command interface {
	Name() string
	Description() string
	Run(ctx context.Context, inv *Invocation) error
}
