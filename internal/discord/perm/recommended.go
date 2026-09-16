package perm

import "github.com/disgoorg/disgo/discord"

// recommendedBot is what the bot asks for in its invite link, in the order the
// README lists them.
//
// One list, two readers. The invite URL needs the bits ORed together and the
// README needs the names. Maintained separately, they drifted: the mask asked
// for eight permissions while the prose promised five, so anyone reading the
// README to decide what they were granting was told wrong. Deriving both from
// here means the next permission added shows up in both places or neither.
var recommendedBot = []int64{
	// Seeing the channel a command came from, and posting "Playback failed"
	// there when there is no status message to edit.
	int64(discord.PermissionViewChannel),
	int64(discord.PermissionSendMessages),
	int64(discord.PermissionEmbedLinks),
	// VoicePlayback, which /play and /next check before joining.
	int64(discord.PermissionConnect),
	int64(discord.PermissionSpeak),
}

// Nothing else is asked for, because nothing else is used: interaction replies
// -- including /about's banner and export-data's file -- are answered through
// the interaction, not posted as the bot, so they need no channel permission.
// The set used to add Attach Files, Read Message History, Manage Messages,
// Manage Roles and Use Application Commands, none of which any code path
// exercises, while leaving out the two voice permissions playback refuses to
// start without.

// RecommendedBotMask is the permissions bitmask for the OAuth2 invite URL.
func RecommendedBotMask() int64 {
	var mask int64
	for _, bit := range recommendedBot {
		mask |= bit
	}
	return mask
}

// RecommendedBotNames is the same set in Discord's own wording, for telling a
// server admin what they are about to grant.
func RecommendedBotNames() []string {
	names := make([]string, 0, len(recommendedBot))
	for _, bit := range recommendedBot {
		names = append(names, Name(bit))
	}
	return names
}

// VoicePlayback is what the bot needs in a voice channel before it can play.
// The invite asks for exactly these, and TestTheInviteGrantsWhatPlaybackChecks
// holds the two together.
var VoicePlayback = discord.PermissionConnect | discord.PermissionSpeak
