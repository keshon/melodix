// Package cmd provides a transport-agnostic command core: a command is something
// with a name, description, and Run(ctx, invocation). How it is registered and
// dispatched (Discord slash, CLI, HTTP) is defined by adapters that wrap this.
package cmd

import "context"

// Invocation carries the minimal input any command runner can pass: arguments
// and an opaque payload. Adapters set Data to their context (e.g. *discordgo.Session
// + event, or *flag.FlagSet + CLI context).
type Invocation struct {
	Args []string
	Data interface{}
}

// Command is the universal contract: identity plus execution. Permissions, flags,
// subcommands, and transport-specific registration stay in adapters.
type Command interface {
	Name() string
	Description() string
	Run(ctx context.Context, inv *Invocation) error
}
