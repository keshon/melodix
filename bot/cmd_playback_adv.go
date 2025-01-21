// commands/playback.go
package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/player"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("list", listCommand)
	registerCommand("pause", pauseResumeCommand)
}

func listCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	instance := b.getOrCreatePlayer(m.GuildID)
	songs := instance.Queue
	emb := embed.NewEmbed().SetColor(embedColor).SetDescription("Queue")
	if len(songs) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Queue is empty.").MessageEmbed)
		return
	}
	for index, song := range songs {
		hostname, err := extractHostname(song.PublicLink)
		if err != nil {
			hostname = song.Source.String()
		}

		emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%d. %s", index+1, song.Title),
			Value:  fmt.Sprintf("[%s](%s)", hostname, song.PublicLink),
			Inline: false,
		})
	}
	s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
}

func pauseResumeCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	instance := b.getOrCreatePlayer(m.GuildID)
	voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
	if err != nil || voiceState.ChannelID == "" {
		emb := embed.NewEmbed().SetColor(embedColor)
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("You must be in a voice channel to use this command.").MessageEmbed)
		return
	}
	if instance.ChannelID != voiceState.ChannelID {
		instance.ChannelID = voiceState.ChannelID
		instance.ActionSignals <- player.ActionSwap
	} else {
		instance.ActionSignals <- player.ActionPauseResume
	}
}
