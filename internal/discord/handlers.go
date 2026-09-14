package discord

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/keshon/command"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/cmdlogger"
	"github.com/keshon/melodix/internal/discord/cmdsync"
	"github.com/keshon/melodix/internal/discord/reply"
)

// interactionInvoker reads the caller off an interaction, once, so nothing
// downstream has to hold the event to ask again.
//
// A guild interaction carries Member, a direct message carries User, and
// neither is guaranteed. Getting the order wrong means the audit log names the
// wrong person, or nobody.
func interactionInvoker(i discord.Interaction) cmdadapter.Invoker {
	who := cmdadapter.Invoker{
		UserID:   cmdadapter.UnknownUserID,
		Username: cmdadapter.UnknownUsername,
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
	}
	if user.ID != 0 {
		who.UserID = user.ID.String()
		who.Username = user.Username
	}
	return who
}

// onReady fires on every successful connect/reconnect.
func (b *Bot) onReady(e *events.Ready, syncer *cmdsync.Syncer) {
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
func (b *Bot) onGuildJoin(e *events.GuildJoin, syncer *cmdsync.Syncer) {
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
	syncer *cmdsync.Syncer,
	logger *cmdlogger.Logger,
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

	var inv *command.Invocation
	switch data := e.Data.(type) {
	case discord.SlashCommandInteractionData:
		inv = &command.Invocation{Data: &cmdadapter.SlashInteractionContext{
			Invoker: who, Responder: responder, API: api,
			Arguments: reply.SlashArguments(data),
			Storage:   b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
			Syncer: syncer,
		}}
	case discord.MessageCommandInteractionData:
		inv = &command.Invocation{Data: &cmdadapter.MessageApplicationCommandContext{
			Invoker: who, Responder: responder, API: api,
			Storage: b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
		}}
	default:
		return
	}

	b.runGuardedInteraction(responder, "slash", name, func(cmdCtx context.Context) error {
		return c.Run(cmdCtx, inv)
	})
}

// onComponentInteraction dispatches a click on a message component.
func (b *Bot) onComponentInteraction(e *events.ComponentInteractionCreate, logger *cmdlogger.Logger) {
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

	handler, ok := command.Root(matched).(cmdadapter.ComponentInteractionHandler)
	if !ok {
		b.log.Warn().Str("command", matched.Name()).Msg("component_handler_missing")
		return
	}

	responder := reply.NewComponentResponder(e)

	b.runGuardedInteraction(responder, "component", matched.Name(), func(cmdCtx context.Context) error {
		_ = cmdCtx
		return handler.Component(&cmdadapter.ComponentInteractionContext{
			Invoker:     interactionInvoker(e),
			Responder:   responder,
			API:         reply.NewSessionAPI(e.Client()),
			ComponentID: customID,
			Storage:     b.storage, Config: b.cfg, Logger: logger, AppLog: b.log,
		})
	})
}

// onMessageCreate handles @mention messages directed at the bot.
func (b *Bot) onMessageCreate(e *events.MessageCreate) {
	self, ok := e.Client().Caches.SelfUser()
	if !ok || e.Message.Author.ID == self.ID {
		return
	}
	mentioned := false
	for _, u := range e.Message.Mentions {
		if u.ID == self.ID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	api := reply.NewSessionAPI(e.Client())
	who := cmdadapter.Invoker{
		ChannelID: e.ChannelID.String(),
		UserID:    e.Message.Author.ID.String(),
		Username:  e.Message.Author.Username,
	}
	if e.GuildID != nil {
		who.GuildID = e.GuildID.String()
	}

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			b.log.Warn().Str("kind", "message").Err(err).Msg("command_slot_busy")
		},
	}, func(cmdCtx context.Context) error {
		inv := &command.Invocation{Data: &cmdadapter.MessageContext{
			Invoker: who, API: api, Storage: b.storage, Config: b.cfg,
		}}
		for _, c := range command.DefaultRegistry.GetAll() {
			if err := c.Run(cmdCtx, inv); err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					b.log.Warn().Str("kind", "message").Err(err).Msg("command_timeout")
					_ = api.SendChannelEmbed(who.ChannelID, &cmdadapter.Embed{
						Description: "Timed out running command.",
					})
					continue
				}
				b.log.Error().Str("kind", "message").Err(err).Msg("command_run_error")
				_ = api.SendChannelEmbed(who.ChannelID, &cmdadapter.Embed{
					Description: fmt.Sprintf("Error: %v", err),
				})
			}
		}
		return nil
	})
}
