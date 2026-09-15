package discord

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/keshon/command"

	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/audit"
	"github.com/keshon/melodix/internal/discord/reply"
	"github.com/keshon/melodix/internal/discord/slashsync"
)

// interactionInvoker reads the caller off an interaction, once, so nothing
// downstream has to hold the event to ask again.
//
// A guild interaction carries Member, a direct message carries User, and
// neither is guaranteed. Getting the order wrong means the audit log names the
// wrong person, or nobody.
func interactionInvoker(i discord.Interaction) adapter.Invoker {
	who := adapter.Invoker{
		UserID:   adapter.UnknownUserID,
		Username: adapter.UnknownUsername,
	}
	if gid := i.GuildID(); gid != nil {
		who.GuildID = gid.String()
	}
	if cid := i.Channel().ID(); cid != 0 {
		who.ChannelID = cid.String()
	}

	user := i.User()
	if member := i.Member(); member != nil {
		user = member.User
		// Discord computed these for this channel, overwrites included. Taking
		// them from here rather than from the member cache is what makes a
		// permission check independent of which events the bot happens to
		// subscribe to.
		who.Permissions = int64(member.Permissions)
		who.PermissionsKnown = true
	}
	if user.ID != 0 {
		who.UserID = user.ID.String()
		who.Username = user.Username
	}
	return who
}

// onReady fires on every successful connect/reconnect.
func (b *Bot) onReady(e *events.Ready, syncer *slashsync.Syncer) {
	for _, g := range e.Guilds {
		guildID := g.ID.String()
		if b.isGuildBlacklisted(guildID) {
			b.log.Info().Str("guild_id", guildID).Msg("guild_blacklisted_leaving")
			if err := e.Client().Rest.LeaveGuild(g.ID); err != nil {
				b.log.Error().Str("guild_id", guildID).Err(err).Msg("guild_leave_failed")
			}
			continue
		}
		if b.cfg.InitSlashCommands {
			if err := syncer.SyncGuildCommands(guildID); err != nil {
				b.log.Error().Str("guild_id", guildID).Err(err).Msg("commands_sync_failed")
			}
		}
	}
	b.log.Info().Str("username", e.User.Username).Msg("discord_ready")
}

// onGuildJoin fires when the bot joins a new guild.
func (b *Bot) onGuildJoin(e *events.GuildJoin, syncer *slashsync.Syncer) {
	guildID := e.Guild.ID.String()
	b.log.Info().Str("guild_id", guildID).Str("guild_name", e.Guild.Name).Msg("guild_added")

	if b.isGuildBlacklisted(guildID) {
		b.log.Info().Str("guild_id", guildID).Msg("guild_blacklisted_leaving")
		if err := e.Client().Rest.LeaveGuild(e.Guild.ID); err != nil {
			b.log.Error().Str("guild_id", guildID).Err(err).Msg("guild_leave_failed")
		}
		return
	}
	if b.cfg.InitSlashCommands {
		if err := syncer.SyncGuildCommands(guildID); err != nil {
			b.log.Error().Str("guild_id", guildID).Err(err).Msg("commands_sync_failed")
		}
	}
}

// onApplicationCommand dispatches slash and context-menu commands.
func (b *Bot) onApplicationCommand(
	e *events.ApplicationCommandInteractionCreate,
	syncer *slashsync.Syncer,
	recorder *audit.Recorder,
) {
	name := e.Data.CommandName()
	c := command.DefaultRegistry.Get(name)
	if c == nil {
		b.log.Warn().Str("command", name).Msg("command_unknown")
		return
	}

	responder := reply.NewCommandResponder(e)
	api := reply.NewSessionAPI(e.Client())
	who := interactionInvoker(e)

	// Slash only. A context-menu registration would need a command that
	// declares one, and nothing has ever declared one here.
	data, ok := e.Data.(discord.SlashCommandInteractionData)
	if !ok {
		b.log.Warn().Str("command", name).Str("kind", fmt.Sprintf("%T", e.Data)).
			Msg("interaction_kind_unhandled")
		return
	}
	inv := &command.Invocation{Data: &adapter.SlashInteractionContext{
		Invoker: who, Responder: responder, API: api,
		Arguments: reply.SlashArguments(data),
		Storage:   b.storage, Config: b.cfg, Audit: recorder, AppLog: b.log,
		Syncer: syncer,
	}}

	b.dispatchInteraction(who, responder, "slash", name, func(cmdCtx context.Context) error {
		return c.Run(cmdCtx, inv)
	})
}

// onComponentInteraction dispatches a click on a message component.
func (b *Bot) onComponentInteraction(e *events.ComponentInteractionCreate, recorder *audit.Recorder) {
	customID := e.Data.CustomID()
	b.log.Debug().Str("custom_id", customID).Msg("component_interaction")

	var matched command.Command
	for _, c := range command.DefaultRegistry.GetAll() {
		if matchesComponentID(customID, c.Name()) {
			matched = c
			break
		}
	}
	if matched == nil {
		b.log.Warn().Str("custom_id", customID).Msg("component_no_handler")
		return
	}

	handler, ok := command.Root(matched).(adapter.ComponentInteractionHandler)
	if !ok {
		b.log.Warn().Str("command", matched.Name()).Msg("component_handler_missing")
		return
	}

	responder := reply.NewComponentResponder(e)
	who := interactionInvoker(e)

	b.dispatchInteraction(who, responder, "component", matched.Name(), func(cmdCtx context.Context) error {
		_ = cmdCtx
		return handler.Component(&adapter.ComponentInteractionContext{
			Invoker:     who,
			Responder:   responder,
			API:         reply.NewSessionAPI(e.Client()),
			ComponentID: customID,
			Storage:     b.storage, Config: b.cfg, Audit: recorder, AppLog: b.log,
		})
	})
}
