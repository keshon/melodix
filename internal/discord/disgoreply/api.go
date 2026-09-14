package disgoreply

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// The connection-level answers. Everything here reads from disgo's cache
// first and falls back to REST only where the cache cannot answer at all,
// which is the same order the discordgo backend uses and for the same reason:
// these run on the command path.

// MemberPermissions resolves a caller's effective permissions in a channel.
//
// discordgo computed this itself from roles and overwrites on every call;
// disgo's cache does the same resolution behind MemberPermissionsInChannel.
// Both answer from cached state, so both are only as current as the cache --
// which is what a permission check on the command path wants, because the
// alternative is two API calls before every refusal.
func (a *API) MemberPermissions(userID, channelID string) (int64, error) {
	if a.client == nil {
		return 0, nil
	}
	uid, err := parseID(userID)
	if err != nil {
		return 0, nil
	}
	cid, err := parseID(channelID)
	if err != nil {
		return 0, nil
	}
	channel, ok := a.client.Caches.Channel(cid)
	if !ok {
		return 0, fmt.Errorf("channel %s not in cache", channelID)
	}
	member, ok := a.client.Caches.Member(channel.GuildID(), uid)
	if !ok {
		return 0, fmt.Errorf("member %s not in cache", userID)
	}
	return int64(a.client.Caches.MemberPermissionsInChannel(channel, member)), nil
}

// CheckBotPermissions reports whether the bot may manage messages here.
func (a *API) CheckBotPermissions(channelID string) bool {
	perms, err := a.botPermissions(channelID)
	if err != nil {
		return false
	}
	return perms.Has(discord.PermissionManageMessages)
}

// CheckBotVoicePermissions reports whether the bot may connect and speak.
// Asked before playback so a refusal is a message rather than a silent
// failure to join.
func (a *API) CheckBotVoicePermissions(channelID string) (bool, error) {
	perms, err := a.botPermissions(channelID)
	if err != nil {
		return false, err
	}
	return perms.Has(discord.PermissionConnect, discord.PermissionSpeak), nil
}

func (a *API) botPermissions(channelID string) (discord.Permissions, error) {
	if a.client == nil {
		return 0, fmt.Errorf("no Discord session")
	}
	cid, err := parseID(channelID)
	if err != nil {
		return 0, err
	}
	channel, ok := a.client.Caches.Channel(cid)
	if !ok {
		return 0, fmt.Errorf("channel %s not in cache", channelID)
	}
	self, ok := a.client.Caches.SelfMember(channel.GuildID())
	if !ok {
		return 0, fmt.Errorf("bot member not in cache for guild %s", channel.GuildID())
	}
	return a.client.Caches.MemberPermissionsInChannel(channel, self), nil
}

func (a *API) SendChannelMessage(channelID, content string) error {
	cid, err := a.channelID(channelID)
	if err != nil {
		return err
	}
	_, err = a.client.Rest.CreateMessage(cid, discord.MessageCreate{Content: content})
	return err
}

func (a *API) SendChannelEmbed(channelID string, embed *cmdadapter.Embed) error {
	cid, err := a.channelID(channelID)
	if err != nil {
		return err
	}
	_, err = a.client.Rest.CreateMessage(cid, discord.MessageCreate{Embeds: Embeds(embed)})
	return err
}

func (a *API) EditChannelEmbed(channelID, messageID string, embed *cmdadapter.Embed) error {
	cid, err := a.channelID(channelID)
	if err != nil {
		return err
	}
	mid, err := parseID(messageID)
	if err != nil {
		return err
	}
	embeds := []discord.Embed{Embed(embed)}
	_, err = a.client.Rest.UpdateMessage(cid, mid, discord.MessageUpdate{Embeds: &embeds})
	return err
}

// GuildInfo describes a guild. The counts are whatever the cache holds, which
// is what the status command has always reported.
func (a *API) GuildInfo(guildID string) (cmdadapter.GuildInfo, error) {
	if a.client == nil {
		return cmdadapter.GuildInfo{}, fmt.Errorf("no Discord session")
	}
	gid, err := parseID(guildID)
	if err != nil {
		return cmdadapter.GuildInfo{}, err
	}
	guild, ok := a.client.Caches.Guild(gid)
	if !ok {
		fetched, err := a.client.Rest.GetGuild(gid, false)
		if err != nil {
			return cmdadapter.GuildInfo{}, err
		}
		guild = fetched.Guild
	}

	channels := 0
	for ch := range a.client.Caches.Channels() {
		if ch.GuildID() == gid {
			channels++
		}
	}

	return cmdadapter.GuildInfo{
		ID:       guild.ID.String(),
		Name:     guild.Name,
		Members:  a.client.Caches.MembersLen(gid),
		Roles:    a.client.Caches.RolesLen(gid),
		Channels: channels,
	}, nil
}

// UserVoiceChannel is the voice channel a user is connected to.
func (a *API) UserVoiceChannel(guildID, userID string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("no Discord session")
	}
	gid, err := parseID(guildID)
	if err != nil {
		return "", err
	}
	uid, err := parseID(userID)
	if err != nil {
		return "", err
	}
	state, ok := a.client.Caches.VoiceState(gid, uid)
	if !ok || state.ChannelID == nil {
		return "", fmt.Errorf("user not in any voice channel")
	}
	return state.ChannelID.String(), nil
}

func (a *API) channelID(channelID string) (snowflake.ID, error) {
	if a.client == nil {
		return 0, fmt.Errorf("no Discord session")
	}
	return parseID(channelID)
}

// parseID turns one of the string ids the neutral layer speaks back into a
// snowflake. The seam uses strings because that is the one spelling both
// libraries agree on -- discordgo has no id type at all -- so every disgo
// entry point converts here.
func parseID(s string) (snowflake.ID, error) {
	if s == "" {
		return 0, fmt.Errorf("empty id")
	}
	id, err := snowflake.Parse(s)
	if err != nil {
		return 0, fmt.Errorf("parsing id %q: %w", s, err)
	}
	return id, nil
}
