package commandsync

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
)

const discordRateLimitDelay = 25 * time.Millisecond

// Syncer handles registering and syncing slash commands per guild.
type Syncer struct {
	dg       *discordgo.Session
	registry *commandkit.Registry

	// perGuildLocks serializes sync operations per guild.
	// Kept inside Syncer (not global) so multiple Syncer instances don't share state.
	perGuildLocks sync.Map // map[guildID string]*sync.Mutex
}

// NewSyncer creates a command syncer with a Discord session and command registry.
func NewSyncer(dg *discordgo.Session, registry *commandkit.Registry) *Syncer {
	return &Syncer{
		dg:       dg,
		registry: registry,
	}
}

// SyncGuildCommands syncs commands for a guild by comparing desired definitions (registry)
// with actual commands in Discord, then creating, editing, and deleting as needed.
func (m *Syncer) SyncGuildCommands(guildID string) error {
	mu := m.guildLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	appID, err := m.appID()
	if err != nil {
		return err
	}

	existingCmds, err := m.dg.ApplicationCommands(appID, guildID)
	if err != nil {
		return fmt.Errorf("failed to list application commands: %w", err)
	}
	desiredCmds := m.buildCommandDefinitions()

	existingByKey := make(map[string]*discordgo.ApplicationCommand, len(existingCmds))
	for _, c := range existingCmds {
		existingByKey[commandKey(c)] = c
	}
	desiredByKey := make(map[string]*discordgo.ApplicationCommand, len(desiredCmds))
	for _, c := range desiredCmds {
		desiredByKey[commandKey(c)] = c
	}

	var created, edited, deleted, unchanged int

	log.Printf("[INFO] [%s] Syncing commands (desired=%d existing=%d)", guildID, len(desiredCmds), len(existingCmds))

	for key, desired := range desiredByKey {
		if existing, ok := existingByKey[key]; ok {
			if commandFingerprint(existing) == commandFingerprint(desired) {
				unchanged++
				continue
			}
			if _, err := m.dg.ApplicationCommandEdit(appID, guildID, existing.ID, desired); err != nil {
				log.Printf("[ERR] [%s] Failed to edit command %q (type %d): %v", guildID, desired.Name, desired.Type, err)
			} else {
				edited++
			}
			time.Sleep(discordRateLimitDelay)
			continue
		}

		if _, err := m.dg.ApplicationCommandCreate(appID, guildID, desired); err != nil {
			log.Printf("[ERR] [%s] Failed to create command %q (type %d): %v", guildID, desired.Name, desired.Type, err)
		} else {
			created++
		}
		time.Sleep(discordRateLimitDelay)
	}

	for key, existing := range existingByKey {
		if _, ok := desiredByKey[key]; ok {
			continue
		}
		if err := m.dg.ApplicationCommandDelete(appID, guildID, existing.ID); err != nil {
			log.Printf("[ERR] [%s] Failed to delete obsolete command %q (type %d): %v", guildID, existing.Name, existing.Type, err)
		} else {
			deleted++
		}
		time.Sleep(discordRateLimitDelay)
	}

	log.Printf("[DONE] [%s] Commands sync result: created=%d edited=%d deleted=%d unchanged=%d", guildID, created, edited, deleted, unchanged)

	return nil
}

// SyncAllGuilds syncs commands for every guild the bot is currently in.
func (m *Syncer) SyncAllGuilds() {
	if m.dg == nil {
		return
	}
	for _, g := range m.dg.State.Guilds {
		if err := m.SyncGuildCommands(g.ID); err != nil {
			log.Printf("[ERR] Failed to sync commands for guild %s: %v", g.ID, err)
		}
	}
}

// --- Internal helpers ---

func (m *Syncer) guildLock(guildID string) *sync.Mutex {
	v, _ := m.perGuildLocks.LoadOrStore(guildID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (m *Syncer) buildCommandDefinitions() []*discordgo.ApplicationCommand {
	var defs []*discordgo.ApplicationCommand
	for _, c := range m.registry.GetAll() {
		if def := toApplicationCommand(c); def != nil {
			defs = append(defs, def)
		}
	}
	return defs
}

// --- Command conversion ---

func toApplicationCommand(c commandkit.Command) *discordgo.ApplicationCommand {
	root := commandkit.Root(c)

	if slash, ok := root.(command.SlashProvider); ok {
		if def := slash.SlashDefinition(); def != nil {
			if def.Type == 0 {
				def.Type = discordgo.ChatApplicationCommand
			}
			return def
		}
	}

	if menu, ok := root.(command.ContextMenuProvider); ok {
		if def := menu.ContextDefinition(); def != nil {
			if def.Type == 0 {
				def.Type = discordgo.MessageApplicationCommand
			}
			return def
		}
	}

	return nil
}

func commandKey(c *discordgo.ApplicationCommand) string {
	return fmt.Sprintf("%s:%d", c.Name, c.Type)
}

func (m *Syncer) appID() (string, error) {
	if id := m.dg.State.User.ID; id != "" {
		return id, nil
	}
	u, err := m.dg.User("@me")
	if err != nil {
		return "", fmt.Errorf("failed to fetch bot user: %w", err)
	}
	return u.ID, nil
}

