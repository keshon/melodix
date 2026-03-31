package cmdmanager

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/storage"
)

const discordRateLimitDelay = 25 * time.Millisecond

// Manager handles registering and syncing slash commands per guild.
type Manager struct {
	dg       *discordgo.Session
	storage  *storage.Storage
	registry *commandkit.Registry

	// perGuildLocks serializes read-modify-write of command hash cache per guild.
	// Kept inside Manager (not global) so multiple Manager instances don't share state.
	perGuildLocks sync.Map // map[guildID string]*sync.Mutex
}

// NewManager creates a command manager with a Discord session, storage, and command registry.
func NewManager(dg *discordgo.Session, storage *storage.Storage, registry *commandkit.Registry) *Manager {
	return &Manager{
		dg:       dg,
		storage:  storage,
		registry: registry,
	}
}

// RegisterCommands syncs commands for a guild: deletes obsolete ones, creates or updates changed ones.
func (m *Manager) RegisterCommands(guildID string) error {
	mu := m.guildLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	appID, err := m.appID()
	if err != nil {
		return err
	}

	registeredCmds, _ := m.dg.ApplicationCommands(appID, guildID)
	definedCmds := m.buildCommandDefinitions()

	cachedHashes, _ := m.storage.CommandHashes(guildID)
	if cachedHashes == nil {
		cachedHashes = map[string]string{}
	}

	m.deleteObsoleteCommands(appID, guildID, registeredCmds, definedCmds)
	m.upsertChangedCommands(appID, guildID, definedCmds, cachedHashes)

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

// deleteObsoleteCommands removes from Discord any commands that are no longer in the local registry.
func (m *Manager) deleteObsoleteCommands(
	appID, guildID string,
	registeredCmds, definedCmds []*discordgo.ApplicationCommand,
) {
	definedKeys := make(map[string]struct{}, len(definedCmds))
	for _, d := range definedCmds {
		definedKeys[commandKey(d)] = struct{}{}
	}

	hashes, _ := m.storage.CommandHashes(guildID)
	if hashes == nil {
		hashes = map[string]string{}
	}

	for _, rc := range registeredCmds {
		if _, stillDefined := definedKeys[commandKey(rc)]; stillDefined {
			continue
		}
		log.Printf("[INFO] [%s] Deleting obsolete command: %s (type %d)", guildID, rc.Name, rc.Type)
		if err := m.dg.ApplicationCommandDelete(appID, guildID, rc.ID); err != nil {
			log.Printf("[ERR] [%s] Failed to delete command %q: %v", guildID, rc.Name, err)
		} else {
			delete(hashes, rc.Name)
		}
	}

	_ = m.storage.SetCommandHashes(guildID, hashes)
}

// upsertChangedCommands creates or updates commands whose hash differs from the cached value.
// It receives cachedHashes from the caller to avoid a redundant storage read.
func (m *Manager) upsertChangedCommands(
	appID, guildID string,
	defs []*discordgo.ApplicationCommand,
	cachedHashes map[string]string,
) {
	var changed []*discordgo.ApplicationCommand
	freshHashes := make(map[string]string, len(defs))

	for _, d := range defs {
		h := hashCommand(d)
		freshHashes[d.Name] = h
		if cachedHashes[d.Name] != h {
			changed = append(changed, d)
		}
	}

	if len(changed) == 0 {
		return
	}

	log.Printf("[INFO] [%s] Registering %d changed command(s)...", guildID, len(changed))

	for _, d := range changed {
		if _, err := m.dg.ApplicationCommandCreate(appID, guildID, d); err != nil {
			log.Printf("[ERR] [%s] Failed to register command %q: %v", guildID, d.Name, err)
		} else {
			log.Printf("[DONE] [%s] Registered command: %q", guildID, d.Name)
		}
		time.Sleep(discordRateLimitDelay)
	}

	// Merge fresh hashes into the stored ones so obsolete entries are preserved
	// until deleteObsoleteCommands cleans them up.
	storedHashes, _ := m.storage.CommandHashes(guildID)
	if storedHashes == nil {
		storedHashes = map[string]string{}
	}
	for k, v := range freshHashes {
		storedHashes[k] = v
	}
	_ = m.storage.SetCommandHashes(guildID, storedHashes)
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
