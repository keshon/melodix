package discord

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/readme"
)

// onReady fires on every successful connect/reconnect.
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	botInfo, err := s.User("@me")
	if err != nil {
		b.log.Warn().Err(err).Msg("bot_user_fetch_failed")
		return
	}

	for _, g := range r.Guilds {
		if b.isGuildBlacklisted(g.ID) {
			b.log.Info().Str("guild_id", g.ID).Msg("guild_blacklisted_leaving")
			if err := s.GuildLeave(g.ID); err != nil {
				b.log.Error().Str("guild_id", g.ID).Err(err).Msg("guild_leave_failed")
			}
			continue
		}
		if b.cfg.InitSlashCommands {
			if err := b.cmdSyncer.SyncGuildCommands(g.ID); err != nil {
				b.log.Error().Str("guild_id", g.ID).Err(err).Msg("commands_sync_failed")
			}
		}
	}

	// Background services start once across all reconnects.
	b.once.Do(func() {
		b.log.Info().Msg("bg_services_started")
		if err := readme.UpdateReadme(commandkit.DefaultRegistry, config.CategoryWeights, b.log); err != nil {
			b.log.Error().Err(err).Msg("readme_update_failed")
		}
	})

	b.log.Info().Str("username", botInfo.Username).Msg("discord_ready")
}

// onGuildCreate fires when the bot joins a new guild.
func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	b.log.Info().Str("guild_id", g.Guild.ID).Str("guild_name", g.Guild.Name).Msg("guild_added")
	if b.isGuildBlacklisted(g.Guild.ID) {
		b.log.Info().Str("guild_id", g.Guild.ID).Msg("guild_blacklisted_leaving")
		if err := s.GuildLeave(g.Guild.ID); err != nil {
			b.log.Error().Str("guild_id", g.Guild.ID).Err(err).Msg("guild_leave_failed")
		}
		return
	}
	if b.cfg.InitSlashCommands {
		if err := b.cmdSyncer.SyncGuildCommands(g.Guild.ID); err != nil {
			b.log.Error().Str("guild_id", g.Guild.ID).Err(err).Msg("commands_sync_failed")
		}
	}
}
