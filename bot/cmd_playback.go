// commands/playback.go
package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/player"
	songpkg "github.com/keshon/melodix/song"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("play", playCommand)
	registerCommand("skip", skipCommand)
	registerCommand("stop", stopCommand)
}

func playCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	err := b.saveCommandHistory(m.GuildID, m.ChannelID, m.Author.ID, m.Author.Username, command, param)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error saving command info: %v", err))
		return
	}

	voiceState, err := b.findUserVoiceState(m.GuildID, m.Author.ID)
	emb := embed.NewEmbed().SetColor(embedColor)
	if err != nil || voiceState.ChannelID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("You must be in a voice channel to use this command.").MessageEmbed)
		return
	}

	b.playMessage[m.GuildID], _ = s.ChannelMessageSendEmbed(m.ChannelID, emb.SetDescription("Please wait...").MessageEmbed)
	b.playChannelID[m.GuildID] = m.ChannelID

	// extract "kkdai " or "ytdlp " from the beginning of the param
	var parser songpkg.Parser
	if strings.HasPrefix(param, songpkg.ParserKkdai.String()+" ") {
		parser = songpkg.ParserKkdai
		param = param[len(songpkg.ParserKkdai)+1:]
	}
	if strings.HasPrefix(param, songpkg.ParserYtdlp.String()+" ") {
		parser = songpkg.ParserYtdlp
		param = param[len(songpkg.ParserYtdlp)+1:]
	}

	fmt.Println(param, parser)

	songs, err := b.fetchSongs(param, parser)
	if err != nil {
		s.ChannelMessageEditEmbed(m.ChannelID, b.playMessage[m.GuildID].ID, emb.SetDescription(fmt.Sprintf("Error getting this song(s)\n\n%v", err)).MessageEmbed)
		return
	}
	if len(songs) == 0 {
		s.ChannelMessageEditEmbed(m.ChannelID, b.playMessage[m.GuildID].ID, emb.SetDescription("No song found.").MessageEmbed)
		return
	}

	instance := b.getOrCreatePlayer(m.GuildID)
	instance.Queue = append(instance.Queue, songs...)
	instance.GuildID = m.GuildID
	instance.ChannelID = voiceState.ChannelID
	if instance.Song != nil {
		instance.StatusSignals <- player.StatusAdded
		return
	}

	err = instance.Play()
	if err != nil {
		s.ChannelMessageEditEmbed(m.ChannelID, b.playMessage[m.GuildID].ID, emb.SetDescription(fmt.Sprintf("Error playing this song(s)\n\n%v", err)).MessageEmbed)
		b.playMessage[m.GuildID] = nil
		b.playChannelID[m.GuildID] = ""
		return
	}

}

func skipCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	instance := b.getOrCreatePlayer(m.GuildID)
	instance.ActionSignals <- player.ActionSkip
}

func stopCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	instance := b.getOrCreatePlayer(m.GuildID)
	instance.ActionSignals <- player.ActionStop
}
