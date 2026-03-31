package command

import (
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
)

func ConfigFromInvocation(inv *commandkit.Invocation) *config.Config {
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

func Register(discordCmd Handler, mws ...commandkit.Middleware) {
	c := commandkit.Apply(&Adapter{Cmd: discordCmd}, mws...)
	commandkit.DefaultRegistry.Register(c)
}
