package discord

import (
	"context"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/respond"
)

// onMessageCreate handles @mention messages directed at the bot.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	mentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			log.Printf("[WARN] Command slot acquire failed (message): %v", err)
		},
	}, func(cmdCtx context.Context) error {
		inv := &commandkit.Invocation{Data: &command.MessageContext{Session: s, Event: m, Storage: b.storage, Config: b.cfg}}
		for _, c := range commandkit.DefaultRegistry.GetAll() {
			if err := c.Run(cmdCtx, inv); err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					log.Printf("[WARN] Message command timed out: %v", err)
					_ = respond.MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
						Description: "Timed out running command.",
					})
					continue
				}
				log.Println("[ERR] Error running message command:", err)
				_ = respond.MessageEmbed(s, m.ChannelID, &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Error: %v", err),
				})
			}
		}
		return nil
	})
}

// onMessageReactionAdd handles reaction events for commands that use reactions.
func (b *Bot) onMessageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	b.mu.RLock()
	logger := b.cmdLogger
	b.mu.RUnlock()

	b.runWithCommandContext(commandRunOptions{
		onBusy: func(err error) {
			log.Printf("[WARN] Command slot acquire failed (reaction): %v", err)
		},
	}, func(cmdCtx context.Context) error {
		inv := &commandkit.Invocation{Data: &command.MessageReactionContext{
			Session: s, Event: r, Storage: b.storage, Config: b.cfg, Logger: logger,
		}}
		for _, c := range commandkit.DefaultRegistry.GetAll() {
			if _, ok := commandkit.Root(c).(command.ReactionProvider); !ok {
				continue
			}
			if err := c.Run(cmdCtx, inv); err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					log.Printf("[WARN] Reaction command timed out: %v", err)
					continue
				}
				log.Println("[ERR] Error running reaction command:", err)
				_ = respond.MessageEmbed(s, r.ChannelID, &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Error: %v", err),
				})
			}
		}
		return nil
	})
}

