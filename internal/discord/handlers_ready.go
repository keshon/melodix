package discord

import (
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/readme"
)

// onReady fires on every successful connect/reconnect.
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	botInfo, err := s.User("@me")
	if err != nil {
		log.Println("[WARN] Error retrieving bot user:", err)
		return
	}

	for _, g := range r.Guilds {
		if b.isGuildBlacklisted(g.ID) {
			log.Printf("[INFO] Leaving blacklisted guild: %s", g.ID)
			if err := s.GuildLeave(g.ID); err != nil {
				log.Printf("[ERR] Failed to leave guild %s: %v", g.ID, err)
			}
			continue
		}
		if b.cfg.InitSlashCommands {
			if err := b.cmdManager.SyncGuildCommands(g.ID); err != nil {
				log.Printf("[ERR] Error registering slash commands for guild %s: %v", g.ID, err)
			}
		}
	}

	// Background services start once across all reconnects.
	b.once.Do(func() {
		log.Println("[INFO] Starting background services...")
		if err := readme.UpdateReadme(commandkit.DefaultRegistry, config.CategoryWeights); err != nil {
			log.Println("[ERR] Failed to update README:", err)
		}
	})

	log.Printf("[INFO] ✅ Discord bot %v is ready.", botInfo.Username)
}

// onGuildCreate fires when the bot joins a new guild.
func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	log.Printf("[INFO] Bot added to guild: %s (%s)", g.Guild.ID, g.Guild.Name)
	if b.isGuildBlacklisted(g.Guild.ID) {
		log.Printf("[INFO] Leaving blacklisted guild: %s", g.Guild.ID)
		if err := s.GuildLeave(g.Guild.ID); err != nil {
			log.Printf("[ERR] Failed to leave guild %s: %v", g.Guild.ID, err)
		}
		return
	}
	if b.cfg.InitSlashCommands {
		if err := b.cmdManager.SyncGuildCommands(g.Guild.ID); err != nil {
			log.Printf("[ERR] Failed to register commands for guild %s: %v", g.Guild.ID, err)
		}
	}
}

