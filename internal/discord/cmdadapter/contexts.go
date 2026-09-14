package cmdadapter

import (
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/rs/zerolog"
)

// The five kinds of invocation, each carrying what a command may ask of it and
// nothing that names a library. Session and Event used to sit at the top of
// every one of these; what replaced them is Invoker for the questions whose
// answers are fixed, Responder for what can be said back, and API for what
// must be asked of the connection.
//
// The root handlers build these, which is the one place that knows what an
// invocation arrived as.

type SlashInteractionContext struct {
	Invoker   Invoker
	Responder Responder
	API       SessionAPI

	// Arguments are the options this command was invoked with, resolved when
	// the context was built.
	Arguments []SlashArgument

	Args    []string
	Storage *storage.Storage
	Config  *config.Config
	Logger  Logger
	AppLog  zerolog.Logger
	Syncer  CommandSyncer
}

type ComponentInteractionContext struct {
	Invoker   Invoker
	Responder Responder
	API       SessionAPI

	// ComponentID identifies which component was used -- the button's own id,
	// set when the message was built.
	ComponentID string

	Storage *storage.Storage
	Config  *config.Config
	Logger  Logger
	AppLog  zerolog.Logger
}

type MessageReactionContext struct {
	Invoker Invoker
	API     SessionAPI

	Storage *storage.Storage
	Config  *config.Config
	Logger  Logger
}

type MessageApplicationCommandContext struct {
	Invoker   Invoker
	Responder Responder
	API       SessionAPI

	Storage *storage.Storage
	Config  *config.Config
	Logger  Logger
	AppLog  zerolog.Logger
}

type MessageContext struct {
	Invoker Invoker
	API     SessionAPI

	Storage *storage.Storage
	Config  *config.Config
}
