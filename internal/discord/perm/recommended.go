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
	int64(discord.PermissionViewChannel),
	int64(discord.PermissionSendMessages),
	int64(discord.PermissionEmbedLinks),
	int64(discord.PermissionAttachFiles),
	int64(discord.PermissionReadMessageHistory),
	int64(discord.PermissionManageMessages),
	int64(discord.PermissionManageRoles),
	int64(discord.PermissionUseApplicationCommands),
}

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
