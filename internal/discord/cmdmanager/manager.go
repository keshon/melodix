package cmdmanager

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

// Manager handles registering and syncing slash commands per guild.
type Manager struct {
	dg       *discordgo.Session
	registry *commandkit.Registry

	// perGuildLocks serializes sync operations per guild.
	// Kept inside Manager (not global) so multiple Manager instances don't share state.
	perGuildLocks sync.Map // map[guildID string]*sync.Mutex
}

// NewManager creates a command manager with a Discord session, storage, and command registry.
func NewManager(dg *discordgo.Session, registry *commandkit.Registry) *Manager {
	return &Manager{
		dg:       dg,
		registry: registry,
	}
}

// RegisterCommands syncs commands for a guild by comparing desired definitions (registry)
// with actual commands in Discord, then creating, editing, and deleting as needed.
func (m *Manager) RegisterCommands(guildID string) error {
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
			if hashCommand(existing) == hashCommand(desired) {
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

// RefreshAll syncs commands for every guild the bot is currently in.
func (m *Manager) RefreshAll() {
	if m.dg == nil {
		return
	}
	for _, g := range m.dg.State.Guilds {
		if err := m.RegisterCommands(g.ID); err != nil {
			log.Printf("[ERR] Failed to refresh commands for guild %s: %v", g.ID, err)
		}
	}
}

// --- Internal helpers ---

// guildLock returns (creating if needed) a per-guild mutex for hash cache operations.
func (m *Manager) guildLock(guildID string) *sync.Mutex {
	v, _ := m.perGuildLocks.LoadOrStore(guildID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// buildCommandDefinitions converts all registered commands into Discord ApplicationCommand definitions.
func (m *Manager) buildCommandDefinitions() []*discordgo.ApplicationCommand {
	var defs []*discordgo.ApplicationCommand
	for _, c := range m.registry.GetAll() {
		if def := toApplicationCommand(c); def != nil {
			defs = append(defs, def)
		}
	}
	return defs
}

// --- Command conversion ---

// toApplicationCommand converts a commandkit.Command into a Discord ApplicationCommand definition.
// Returns nil if the command does not expose a slash or context-menu definition.
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

// commandKey returns a unique string key for a command based on its name and type.
func commandKey(c *discordgo.ApplicationCommand) string {
	return fmt.Sprintf("%s:%d", c.Name, c.Type)
}

// appID returns the bot's application ID, using the cached state when available.
func (m *Manager) appID() (string, error) {
	if id := m.dg.State.User.ID; id != "" {
		return id, nil
	}
	u, err := m.dg.User("@me")
	if err != nil {
		return "", fmt.Errorf("failed to fetch bot user: %w", err)
	}
	return u.ID, nil
}
