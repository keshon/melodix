package perm

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// Administrator is the bit that overrides every other permission check.
const Administrator int64 = discordgo.PermissionAdministrator

// permissionNames maps a permission bit to the wording Discord's own UI uses,
// so a refusal names the switch the user has to find rather than a number.
//
// It lives here rather than in the middleware that prints it because these are
// Discord's protocol constants, and this package is the layer that is allowed
// to know them. Everything above it deals in int64 bits and asks for a name.
var permissionNames = map[int64]string{
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

// Name returns the display name for a single permission bit, falling back to
// hex so an unrecognised bit still says something specific. Discord adds
// permissions faster than any table tracks them.
func Name(bit int64) string {
	if name, ok := permissionNames[bit]; ok {
		return name
	}
	return fmt.Sprintf("0x%x", bit)
}
