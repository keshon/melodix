package discord

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/melodix/internal/discord/watchdog"
)

func (b *Bot) wireSessionHandlers(dg *discordgo.Session, tracker *watchdog.Tracker) {
	b.configureIntents()
	dg.AddHandler(func(s *discordgo.Session, e *discordgo.Event) {
		_ = s
		_ = e
		tracker.MarkWSNow()
	})
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		tracker.MarkReadyNow()
		b.onReady(s, r)
	})
	dg.AddHandler(b.onGuildCreate)
	dg.AddHandler(b.onMessageCreate)
	dg.AddHandler(b.onMessageReactionAdd)
	dg.AddHandler(b.onInteractionCreate)
}
