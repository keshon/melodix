package perm

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
)

// Administrator is the bit that overrides every other permission check.
const Administrator int64 = int64(discord.PermissionAdministrator)

// permissionNames maps a permission bit to the wording Discord's own UI uses,
// so a refusal names the switch the user has to find rather than a number.
//
// It lives here rather than in the middleware that prints it because these are
// Discord's protocol constants, and this package is the layer that is allowed
// to know them. Everything above it deals in int64 bits and asks for a name.
var permissionNames = map[int64]string{
	int64(discord.PermissionCreateInstantInvite):              "Create Instant Invite",
	int64(discord.PermissionKickMembers):                      "Kick Members",
	int64(discord.PermissionBanMembers):                       "Ban Members",
	int64(discord.PermissionAdministrator):                    "Administrator",
	int64(discord.PermissionManageChannels):                   "Manage Channels",
	int64(discord.PermissionManageGuild):                      "Manage Server",
	int64(discord.PermissionAddReactions):                     "Add Reactions",
	int64(discord.PermissionViewAuditLog):                     "View Audit Logs",
	int64(discord.PermissionViewChannel):                      "View Channel",
	int64(discord.PermissionSendMessages):                     "Send Messages",
	int64(discord.PermissionSendTTSMessages):                  "Send TTS Messages",
	int64(discord.PermissionManageMessages):                   "Manage Messages",
	int64(discord.PermissionEmbedLinks):                       "Embed Links",
	int64(discord.PermissionAttachFiles):                      "Attach Files",
	int64(discord.PermissionReadMessageHistory):               "Read Message History",
	int64(discord.PermissionMentionEveryone):                  "Mention Everyone",
	int64(discord.PermissionUseExternalEmojis):                "Use External Emojis",
	int64(discord.PermissionUseApplicationCommands):           "Use Application Commands",
	int64(discord.PermissionManageThreads):                    "Manage Threads",
	int64(discord.PermissionCreatePublicThreads):              "Create Public Threads",
	int64(discord.PermissionCreatePrivateThreads):             "Create Private Threads",
	int64(discord.PermissionUseExternalStickers):              "Use External Stickers",
	int64(discord.PermissionSendMessagesInThreads):            "Send Messages in Threads",
	int64(discord.PermissionSendVoiceMessages):                "Send Voice Messages",
	int64(discord.PermissionSendPolls):                        "Send Polls",
	int64(discord.PermissionUseExternalApps):                  "Use External Apps",
	int64(discord.PermissionPrioritySpeaker):                  "Priority Speaker",
	int64(discord.PermissionStream):                           "Stream Video",
	int64(discord.PermissionConnect):                          "Connect to Voice Channel",
	int64(discord.PermissionSpeak):                            "Speak",
	int64(discord.PermissionMuteMembers):                      "Mute Members",
	int64(discord.PermissionDeafenMembers):                    "Deafen Members",
	int64(discord.PermissionMoveMembers):                      "Move Members",
	int64(discord.PermissionUseVAD):                           "Use Voice Activity Detection",
	int64(discord.PermissionRequestToSpeak):                   "Request to Speak",
	int64(discord.PermissionUseEmbeddedActivities):            "Use Embedded Activities",
	int64(discord.PermissionUseSoundboard):                    "Use Soundboard",
	int64(discord.PermissionUseExternalSounds):                "Use External Sounds",
	int64(discord.PermissionChangeNickname):                   "Change Nickname",
	int64(discord.PermissionManageNicknames):                  "Manage Nicknames",
	int64(discord.PermissionManageRoles):                      "Manage Roles",
	int64(discord.PermissionManageWebhooks):                   "Manage Webhooks",
	int64(discord.PermissionManageGuildExpressions):           "Manage Expressions (Emojis, Stickers, Sounds)",
	int64(discord.PermissionManageEvents):                     "Manage Events",
	int64(discord.PermissionViewCreatorMonetizationAnalytics): "View Creator Monetization Analytics",
	int64(discord.PermissionCreateGuildExpressions):           "Create Expressions (Emojis, Stickers, Sounds)",
	int64(discord.PermissionCreateEvents):                     "Create Events",
	int64(discord.PermissionViewGuildInsights):                "View Guild Insights",
	int64(discord.PermissionModerateMembers):                  "Moderate Members",
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
