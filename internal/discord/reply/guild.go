package reply

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

// GuildInfo describes a guild in the library-neutral shape commands read.
//
// The cache is asked first and the API only when it has nothing, which is the
// order the status command has always used: the cached guild carries the
// member, role and channel slices this reports, and a fetched one carries
// fewer. Falling back is therefore a downgrade rather than a correction, and
// it happens only when the alternative is no answer at all.
func GuildInfo(s *discordgo.Session, guildID string) (cmdadapter.GuildInfo, error) {
	if s == nil {
		return cmdadapter.GuildInfo{}, fmt.Errorf("no Discord session")
	}

	guild, err := s.State.Guild(guildID)
	if err != nil || guild == nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			return cmdadapter.GuildInfo{}, err
		}
	}

	return cmdadapter.GuildInfo{
		ID:       guild.ID,
		Name:     guild.Name,
		Members:  len(guild.Members),
		Roles:    len(guild.Roles),
		Channels: len(guild.Channels),
	}, nil
}
