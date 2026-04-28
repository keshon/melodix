package common

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/respond"
	"github.com/keshon/melodix/pkg/music/player"
)

// StatusListenTimeout limits how long we listen for status so the goroutine does not leak.
// Updates after the first use the guild's stored message (edit), so they work beyond token expiry.
const StatusListenTimeout = 15 * time.Minute

func ListenPlayerStatusSlash(session *discordgo.Session, event *discordgo.InteractionCreate, p *player.Player, bot discord.VoiceAPI, guildID string) {
	go func() {
		appLog := bot.AppLog()
		ctx, cancel := context.WithTimeout(context.Background(), StatusListenTimeout)
		defer cancel()

		for {
			select {
			case <-ctx.Done():
				return
			case signal, ok := <-p.PlayerStatus:
				if !ok {
					return
				}
				switch signal {
				case player.StatusPlaying:
					track := p.CurrentTrack()
					if track == nil {
						_ = bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
							Title:       "⚠️ Error",
							Description: "Failed to get current track",
						})
						return
					}

					var desc string
					if track.Title != "" && track.URL != "" {
						desc = fmt.Sprintf("🎶 [%s](%s)", track.Title, track.URL)
					} else if track.Title != "" {
						desc = "🎶 " + track.Title
					} else if track.URL != "" {
						desc = "🎶 " + track.URL
					} else {
						desc = "🎶 Unknown track"
					}

					if err := bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
						Title:       player.StatusPlaying.StringEmoji() + " Now Playing",
						Description: desc,
						Color:       respond.EmbedColor,
					}); err != nil {
						appLog.Warn().Str("status", "playing").Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
					}
					return

				case player.StatusAdded:
					if err := bot.UpdateGuildMusicStatus(session, event, guildID, &discordgo.MessageEmbed{
						Title:       player.StatusAdded.StringEmoji() + " Track(s) Added",
						Description: "Added to queue",
						Color:       respond.EmbedColor,
					}); err != nil {
						appLog.Warn().Str("status", "added").Str("guild_id", guildID).Err(err).Msg("guild_status_update_failed")
					}
					return
				}
			}
		}
	}()
}

