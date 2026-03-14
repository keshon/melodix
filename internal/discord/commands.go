package discord

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
)

// commandHashLocks serializes read-modify-write of the command hash cache per guild.
var commandHashLocks sync.Map // guildID string -> *sync.Mutex

func guildCommandHashLock(guildID string) *sync.Mutex {
	v, _ := commandHashLocks.LoadOrStore(guildID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// registerCommands syncs slash commands for a guild with Discord:
// deletes obsolete ones, creates/updates commands whose definition has changed.
func (b *Bot) registerCommands(guildID string) error {
	mu := guildCommandHashLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	appID, err := b.appID()
	if err != nil {
		return err
	}

	remote, _ := b.dg.ApplicationCommands(appID, guildID)
	local := buildCommandDefinitions()
	cachedHashes := loadCommandHashes(guildID)

	b.deleteObsoleteCommands(appID, guildID, remote, local)
	b.upsertChangedCommands(appID, guildID, local, cachedHashes)

	return nil
}

// buildCommandDefinitions returns ApplicationCommand definitions for all registered commands.
func buildCommandDefinitions() []*discordgo.ApplicationCommand {
	var defs []*discordgo.ApplicationCommand
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if def := commandDefinition(c); def != nil {
			defs = append(defs, def)
		}
	}
	return defs
}

// commandKey returns a unique key for matching (name, type) so we delete all obsolete commands,
// including duplicates with the same name but different type (e.g. Chat vs Message).
func commandKey(c *discordgo.ApplicationCommand) string {
	return fmt.Sprintf("%s:%d", c.Name, c.Type)
}

// deleteObsoleteCommands removes commands from Discord that are no longer in the local registry.
// Remote is the full list from Discord; we match by (name, type) so same-name, different-type commands are removed.
func (b *Bot) deleteObsoleteCommands(appID, guildID string, remote []*discordgo.ApplicationCommand, local []*discordgo.ApplicationCommand) {
	localKeys := make(map[string]struct{}, len(local))
	for _, d := range local {
		localKeys[commandKey(d)] = struct{}{}
	}

	hashes := loadCommandHashes(guildID)
	for _, rc := range remote {
		key := commandKey(rc)
		if _, exists := localKeys[key]; exists {
			continue
		}
		log.Printf("[INFO] [%s] Deleting obsolete command: %s (type %d)", guildID, rc.Name, rc.Type)
		if err := b.dg.ApplicationCommandDelete(appID, guildID, rc.ID); err != nil {
			log.Printf("[ERR] [%s] Failed to delete %s: %v", guildID, rc.Name, err)
		} else {
			delete(hashes, rc.Name)
		}
	}
	saveCommandHashes(guildID, hashes)
}

// upsertChangedCommands creates or updates commands whose hash differs from the cached value.
func (b *Bot) upsertChangedCommands(appID, guildID string, defs []*discordgo.ApplicationCommand, cachedHashes map[string]string) {
	var changed []*discordgo.ApplicationCommand
	newHashes := make(map[string]string, len(defs))
	for _, d := range defs {
		h := hashCommand(d)
		newHashes[d.Name] = h
		if cachedHashes[d.Name] != h {
			changed = append(changed, d)
		}
	}
	if len(changed) == 0 {
		return
	}

	log.Printf("[INFO] [%s] Registering %d changed command(s)...", guildID, len(changed))
	for _, d := range changed {
		if _, err := b.dg.ApplicationCommandCreate(appID, guildID, d); err != nil {
			log.Printf("[ERR] [%s] Failed to register %s: %v", guildID, d.Name, err)
		} else {
			log.Printf("[DONE] [%s] Registered: %s", guildID, d.Name)
		}
		time.Sleep(25 * time.Millisecond) // stay well under Discord's rate limit
	}

	merged := loadCommandHashes(guildID)
	for k, v := range newHashes {
		merged[k] = v
	}
	saveCommandHashes(guildID, merged)
}

// handleRefreshCommands processes a SystemEventRefreshCommands event.
func (b *Bot) handleRefreshCommands(evt SystemEvent) {
	appID, err := b.appID()
	if err != nil {
		log.Printf("[ERR][%s] Failed to resolve app ID: %v", evt.GuildID, err)
		return
	}

	if b.isGuildBlacklisted(evt.GuildID) {
		b.removeAllCommands(appID, evt.GuildID)
		return
	}

	switch {
	case strings.HasPrefix(evt.Target, "group:"):
		b.refreshGroup(appID, evt.GuildID, strings.TrimPrefix(evt.Target, "group:"))
	case evt.Target == "" || strings.ToLower(evt.Target) == "all":
		_ = b.registerCommands(evt.GuildID)
	default:
		b.refreshSingle(appID, evt.GuildID, evt.Target)
	}
}

func (b *Bot) removeAllCommands(appID, guildID string) {
	log.Printf("[BLACKLIST][%s] Removing all commands", guildID)
	existing, _ := b.dg.ApplicationCommands(appID, guildID)
	for _, c := range existing {
		if err := b.dg.ApplicationCommandDelete(appID, guildID, c.ID); err != nil {
			log.Printf("[ERR][%s] Failed to delete %s: %v", guildID, c.Name, err)
		} else {
			log.Printf("[DONE][%s] Deleted %s", guildID, c.Name)
		}
	}
}

func (b *Bot) refreshGroup(appID, guildID, group string) {
	disabledGroups, _ := b.storage.GetDisabledGroups(guildID)
	disabled := make(map[string]bool, len(disabledGroups))
	for _, g := range disabledGroups {
		disabled[g] = true
	}

	existing, _ := b.dg.ApplicationCommands(appID, guildID)
	existingByName := make(map[string]*discordgo.ApplicationCommand, len(existing))
	for _, c := range existing {
		existingByName[c.Name] = c
	}

	for _, c := range commandkit.DefaultRegistry.GetAll() {
		meta, ok := commandkit.Root(c).(command.DiscordMeta)
		if !ok || meta.Group() != group {
			continue
		}
		rc, registered := existingByName[c.Name()]
		if disabled[group] && registered {
			log.Printf("[INFO][%s] Removing disabled command: %s", guildID, c.Name())
			_ = b.dg.ApplicationCommandDelete(appID, guildID, rc.ID)
		} else if !disabled[group] && !registered {
			if def := commandDefinition(c); def != nil {
				log.Printf("[INFO][%s] Registering enabled command: %s", guildID, c.Name())
				_, _ = b.dg.ApplicationCommandCreate(appID, guildID, def)
			}
		}
	}
}

func (b *Bot) refreshSingle(appID, guildID, name string) {
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		if strings.EqualFold(c.Name(), name) {
			if def := commandDefinition(c); def != nil {
				_, _ = b.dg.ApplicationCommandCreate(appID, guildID, def)
			}
			return
		}
	}
	log.Printf("[WARN][%s] No command found for refresh target: %s", guildID, name)
}

// commandDefinition extracts the ApplicationCommand definition from a registered command,
// walking through middleware wrappers via commandkit.Root.
func commandDefinition(c commandkit.Command) *discordgo.ApplicationCommand {
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

// appID returns the bot's application ID, fetching from Discord if not cached in State.
func (b *Bot) appID() (string, error) {
	if id := b.dg.State.User.ID; id != "" {
		return id, nil
	}
	u, err := b.dg.User("@me")
	if err != nil {
		return "", fmt.Errorf("failed to fetch bot user: %w", err)
	}
	return u.ID, nil
}

// --- Command hash cache ---

func commandHashPath(guildID string) string {
	return filepath.Join("data", "commands", guildID+".json")
}

func loadCommandHashes(guildID string) map[string]string {
	out := make(map[string]string)
	if data, err := os.ReadFile(commandHashPath(guildID)); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func saveCommandHashes(guildID string, hashes map[string]string) {
	path := commandHashPath(guildID)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	if data, err := json.MarshalIndent(hashes, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}

// --- Command hashing ---

// hashCommand returns a deterministic SHA-1 of a command's stable fields.
// Used to skip re-registration when nothing has changed.
func hashCommand(c *discordgo.ApplicationCommand) string {
	stable := map[string]interface{}{
		"name":        c.Name,
		"description": c.Description,
		"type":        c.Type,
	}
	if len(c.Options) > 0 {
		stable["options"] = normalizeOptions(c.Options)
	}
	data, _ := json.Marshal(stable)
	sum := sha1.Sum(data)
	return fmt.Sprintf("%x", sum)
}

func normalizeOptions(opts []*discordgo.ApplicationCommandOption) []map[string]interface{} {
	out := make([]map[string]interface{}, len(opts))
	for i, o := range opts {
		entry := map[string]interface{}{
			"name":        o.Name,
			"description": o.Description,
			"type":        o.Type,
			"required":    o.Required,
		}
		if len(o.Choices) > 0 {
			choices := make([]map[string]interface{}, len(o.Choices))
			for j, ch := range o.Choices {
				choices[j] = map[string]interface{}{"name": ch.Name, "value": ch.Value}
			}
			entry["choices"] = choices
		}
		if len(o.Options) > 0 {
			entry["options"] = normalizeOptions(o.Options)
		}
		out[i] = entry
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}
