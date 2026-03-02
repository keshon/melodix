package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"

	"github.com/bwmarrin/discordgo"
)

var PermissionNames = map[int64]string{
	discordgo.PermissionCreateInstantInvite:              "Create Instant Invite",
	discordgo.PermissionKickMembers:                      "Kick Members",
	discordgo.PermissionBanMembers:                       "Ban Members",
	discordgo.PermissionAdministrator:                    "Administrator",
	discordgo.PermissionManageChannels:                   "Manage Channels",
	discordgo.PermissionManageGuild:                      "Manage Server",
	discordgo.PermissionAddReactions:                     "Add Reactions",
	discordgo.PermissionViewAuditLogs:                    "View Audit Logs",
	discordgo.PermissionViewChannel:                      "View Channel",
	discordgo.PermissionSendMessages:                     "Send Messages",
	discordgo.PermissionSendTTSMessages:                  "Send TTS Messages",
	discordgo.PermissionManageMessages:                   "Manage Messages",
	discordgo.PermissionEmbedLinks:                       "Embed Links",
	discordgo.PermissionAttachFiles:                      "Attach Files",
	discordgo.PermissionReadMessageHistory:               "Read Message History",
	discordgo.PermissionMentionEveryone:                  "Mention Everyone",
	discordgo.PermissionUseExternalEmojis:                "Use External Emojis",
	discordgo.PermissionUseApplicationCommands:           "Use Application Commands",
	discordgo.PermissionManageThreads:                    "Manage Threads",
	discordgo.PermissionCreatePublicThreads:              "Create Public Threads",
	discordgo.PermissionCreatePrivateThreads:             "Create Private Threads",
	discordgo.PermissionUseExternalStickers:              "Use External Stickers",
	discordgo.PermissionSendMessagesInThreads:            "Send Messages in Threads",
	discordgo.PermissionSendVoiceMessages:                "Send Voice Messages",
	discordgo.PermissionSendPolls:                        "Send Polls",
	discordgo.PermissionUseExternalApps:                  "Use External Apps",
	discordgo.PermissionVoicePrioritySpeaker:             "Priority Speaker",
	discordgo.PermissionVoiceStreamVideo:                 "Stream Video",
	discordgo.PermissionVoiceConnect:                     "Connect to Voice Channel",
	discordgo.PermissionVoiceSpeak:                       "Speak",
	discordgo.PermissionVoiceMuteMembers:                 "Mute Members",
	discordgo.PermissionVoiceDeafenMembers:               "Deafen Members",
	discordgo.PermissionVoiceMoveMembers:                 "Move Members",
	discordgo.PermissionVoiceUseVAD:                      "Use Voice Activity Detection",
	discordgo.PermissionVoiceRequestToSpeak:              "Request to Speak",
	discordgo.PermissionUseEmbeddedActivities:            "Use Embedded Activities",
	discordgo.PermissionUseSoundboard:                    "Use Soundboard",
	discordgo.PermissionUseExternalSounds:                "Use External Sounds",
	discordgo.PermissionChangeNickname:                   "Change Nickname",
	discordgo.PermissionManageNicknames:                  "Manage Nicknames",
	discordgo.PermissionManageRoles:                      "Manage Roles",
	discordgo.PermissionManageWebhooks:                   "Manage Webhooks",
	discordgo.PermissionManageGuildExpressions:           "Manage Expressions (Emojis, Stickers, Sounds)",
	discordgo.PermissionManageEvents:                     "Manage Events",
	discordgo.PermissionViewCreatorMonetizationAnalytics: "View Creator Monetization Analytics",
	discordgo.PermissionCreateGuildExpressions:           "Create Expressions (Emojis, Stickers, Sounds)",
	discordgo.PermissionCreateEvents:                     "Create Events",
	discordgo.PermissionViewGuildInsights:                "View Guild Insights",
	discordgo.PermissionModerateMembers:                  "Moderate Members",
}

func WithUserPermissionCheck() commandkit.Middleware {
	return func(c commandkit.Command) commandkit.Command {
		return commandkit.Wrap(c, func(ctx context.Context, inv *commandkit.Invocation) error {
			var s *discordgo.Session
			var m *discordgo.Member
			var guildID, channelID string

			switch v := inv.Data.(type) {
			case *command.SlashInteractionContext:
				s, m, guildID, channelID = v.Session, v.Event.Member, v.Event.GuildID, v.Event.ChannelID
			case *command.ComponentInteractionContext:
				s, m, guildID, channelID = v.Session, v.Event.Member, v.Event.GuildID, v.Event.ChannelID
			case *command.MessageApplicationCommandContext:
				s, m, guildID, channelID = v.Session, v.Event.Member, v.Event.GuildID, v.Event.ChannelID
			case *command.MessageContext:
				s, m, guildID, channelID = v.Session, v.Event.Member, v.Event.GuildID, v.Event.ChannelID
			default:
				return c.Run(ctx, inv)
			}

			if guildID == "" || m == nil {
				return c.Run(ctx, inv)
			}
			if m.User == nil {
				return c.Run(ctx, inv)
			}

			memberPerms, err := s.UserChannelPermissions(m.User.ID, channelID)
			if err != nil {
				return fmt.Errorf("failed to get user permissions: %w", err)
			}
			if memberPerms&discordgo.PermissionAdministrator != 0 {
				return c.Run(ctx, inv)
			}

			meta, ok := commandkit.Root(c).(command.DiscordMeta)
			if !ok {
				return c.Run(ctx, inv)
			}
			required := meta.UserPermissions()
			if len(required) == 0 {
				return c.Run(ctx, inv)
			}

			hasAny := false
			for _, p := range required {
				if memberPerms&p != 0 {
					hasAny = true
					break
				}
			}
			if !hasAny {
				var allowed []string
				for _, p := range required {
					name := PermissionNames[p]
					if name == "" {
						name = fmt.Sprintf("0x%x", p)
					}
					allowed = append(allowed, name)
				}
				msg := fmt.Sprintf(
					"You need at least one of the following permissions to run this command:\n`%s`",
					strings.Join(allowed, "`, `"),
				)
				switch v := inv.Data.(type) {
				case *command.SlashInteractionContext:
					if v.Responder != nil {
						_ = v.Responder.RespondEmbedEphemeral(s, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				case *command.ComponentInteractionContext:
					if v.Responder != nil {
						_ = v.Responder.RespondEmbedEphemeral(s, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				case *command.MessageApplicationCommandContext:
					if v.Responder != nil {
						_ = v.Responder.RespondEmbedEphemeral(s, v.Event, &discordgo.MessageEmbed{Description: msg})
					}
				case *command.MessageContext:
					_, _ = s.ChannelMessageSend(channelID, msg)
				}
				return nil
			}
			return c.Run(ctx, inv)
		})
	}
}
