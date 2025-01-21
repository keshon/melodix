// commands/playback.go
package bot

import (
	"fmt"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/player"
	songpkg "github.com/keshon/melodix/song"
	embed "github.com/keshon/melodix/third_party/discord_embed"
)

func init() {
	registerCommand("now", nowCommand)
	registerCommand("stats", statsCommand)
	registerCommand("log", logCommand)
}

func nowCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	instance := b.getOrCreatePlayer(m.GuildID)
	if instance.Song != nil {
		emb := embed.NewEmbed().SetColor(embedColor)
		title, source, publicLink, parser, err := instance.Song.GetSongInfo(instance.Song)
		if err != nil {
			s.ChannelMessageEditEmbed(m.ChannelID, b.playMessage[m.GuildID].ID, emb.SetDescription(fmt.Sprintf("Error getting this song(s)\n\n%v", err)).MessageEmbed)
			return
		}
		hostname, err := extractHostname(instance.Song.PublicLink)
		if err != nil {
			hostname = source
		}
		ffmpeg := "`ffmpeg`"
		if parser != "" {
			if parser == songpkg.ParserKkdai.String() {
				parser = fmt.Sprintf("`%s`", "kkdai")
			} else if parser == songpkg.ParserYtdlp.String() {
				parser = fmt.Sprintf("`%s`", "ytdlp")
			}
		}
		emb.SetDescription(fmt.Sprintf("%s Now playing\n\n**%s**\n[%s](%s)\n\n%s %s", player.StatusPlaying.StringEmoji(), title, hostname, publicLink, ffmpeg, parser))
		if len(instance.Song.Thumbnail.URL) > 0 {
			emb.SetThumbnail(instance.Song.Thumbnail.URL)
		}
		emb.SetFooter(fmt.Sprintf("Use %shelp for a list of commands.", b.prefixCache[m.GuildID]))
		s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
		return
	}
	s.ChannelMessageSend(m.ChannelID, "No song is currently playing.")
}

func statsCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	records, err := b.storage.FetchTrackHistory(m.GuildID)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error getting track history: %v", err))
		return
	}
	switch param {
	case "count":
		sort.Slice(records, func(i, j int) bool {
			return records[i].TotalCount > records[j].TotalCount
		})
	case "date", "recent":
		sort.Slice(records, func(i, j int) bool {
			return records[i].LastPlayed.After(records[j].LastPlayed)
		})
	default:
		sort.Slice(records, func(i, j int) bool {
			return records[i].TotalDuration > records[j].TotalDuration
		})
	}
	emb := embed.NewEmbed().SetColor(embedColor).SetDescription("Tracks Statistics").MessageEmbed
	for index, track := range records {
		totalDuration := time.Duration(track.TotalDuration * float64(time.Second))

		hours := int(totalDuration.Hours())
		minutes := int(totalDuration.Minutes()) % 60
		seconds := int(totalDuration.Seconds()) % 60

		durationFormatted := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
		emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%d. %s", index+1, track.Title),
			Value:  fmt.Sprintf("`%s`\t`x%v`\t[%s](%s)", durationFormatted, track.TotalCount, track.SourceType, track.PublicLink),
			Inline: false,
		})
	}
	s.ChannelMessageSendEmbed(m.ChannelID, emb)
}

func logCommand(s *discordgo.Session, m *discordgo.MessageCreate, b *Bot, command, param string) {
	emb := embed.NewEmbed().SetColor(embedColor)
	records, err := b.storage.FetchCommandHistory(m.GuildID)
	if err != nil {
		emb.SetDescription(fmt.Sprintf("Error getting command history: %v", err))
		s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
		return
	}
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	emb.SetDescription("Play History")
	for _, command := range records {
		if command.Command != "play" {
			continue
		}
		emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s - %s", command.Datetime.Format("2006.01.02 15:04:05"), command.Username),
			Value:  command.Param,
			Inline: false,
		})
	}
	s.ChannelMessageSendEmbed(m.ChannelID, emb.MessageEmbed)
}
