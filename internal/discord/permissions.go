package discord

import (
	"github.com/keshon/melodix/internal/config"

	"github.com/bwmarrin/discordgo"
)

// IsAdministrator reports whether a member has administrator privileges in their guild,
// or is the configured developer.
func IsAdministrator(s *discordgo.Session, member *discordgo.Member, cfg *config.Config) bool {
	if member == nil || member.User == nil {
		return false
	}
	if cfg != nil && config.IsDeveloper(cfg, member.User.ID) {
		return true
	}

	guild, err := s.State.Guild(member.GuildID)
	if err != nil || guild == nil {
		guild, err = s.Guild(member.GuildID)
		if err != nil || guild == nil {
			return false
		}
	}

	if member.User.ID == guild.OwnerID {
		return true
	}
	for _, roleID := range member.Roles {
		if role, _ := s.State.Role(guild.ID, roleID); role != nil {
			if role.Permissions&discordgo.PermissionAdministrator != 0 {
				return true
			}
		}
	}
	return false
}

// IsDeveloper reports whether a user ID matches the configured developer.
// Delegates to config for a single source of truth.
func IsDeveloper(cfg *config.Config, userID string) bool {
	return config.IsDeveloper(cfg, userID)
}

// CheckBotPermissions reports whether the bot has ManageMessages permission in a channel.
func CheckBotPermissions(s *discordgo.Session, channelID string) bool {
	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionManageMessages != 0
}
