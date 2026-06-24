package cmdadapter

import (
	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/config"
)

func ConfigFromInvocation(inv *command.Invocation) *config.Config {
	if inv == nil || inv.Data == nil {
		return nil
	}

	switch v := inv.Data.(type) {
	case *SlashInteractionContext:
		return v.Config
	case *ComponentInteractionContext:
		return v.Config
	case *MessageReactionContext:
		return v.Config
	case *MessageApplicationCommandContext:
		return v.Config
	case *MessageContext:
		return v.Config
	default:
		return nil
	}
}

func Register(discordCmd Handler, mws ...command.Middleware) {
	c := command.Apply(&Adapter{Cmd: discordCmd}, mws...)
	command.DefaultRegistry.Register(c)
}
