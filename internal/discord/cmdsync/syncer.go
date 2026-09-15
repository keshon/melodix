// Package cmdsync registers a guild's slash commands.
//
// It compares what the registry declares against what the guild already has,
// then creates, updates and deletes the difference.
package cmdsync

import (
	"fmt"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/reply"
)

// Syncer registers and syncs slash commands per guild.
type Syncer struct {
	client   *bot.Client
	registry *command.Registry
	log      zerolog.Logger

	// perGuildLocks serializes sync operations per guild. Kept inside Syncer
	// (not global) so multiple Syncer instances don't share state.
	//
	// It contends for real, and only because command bodies left the gateway
	// read goroutine: READY and GuildJoin still sync inline there, while
	// /commands enable and /commands disable sync from a command worker. Two
	// goroutines reconciling one guild's command list against the same API is
	// how a guild ends up with a command created twice, or deleted just after
	// it was created.
	perGuildLocks sync.Map // map[guildID string]*sync.Mutex
}

// NewSyncer creates a command syncer with a disgo client and command registry.
func NewSyncer(client *bot.Client, registry *command.Registry, log zerolog.Logger) *Syncer {
	return &Syncer{client: client, registry: registry, log: log}
}

var _ cmdadapter.CommandSyncer = (*Syncer)(nil)

// SyncGuildCommands makes a guild's registered commands match the registry.
func (m *Syncer) SyncGuildCommands(guildID string) error {
	mu := m.guildLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	gid, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parsing guild id %q: %w", guildID, err)
	}

	// The application id is on the client. discordgo had to ask the API for
	// the bot user and read the id off that; there is nothing to ask here.
	appID := m.client.ApplicationID

	existingCmds, err := m.client.Rest.GetGuildCommands(appID, gid, false)
	if err != nil {
		return fmt.Errorf("failed to list application commands: %w", err)
	}
	desired := m.buildCommandDefinitions()

	type existing struct {
		id          snowflake.ID
		name        string
		commandType discord.ApplicationCommandType
		fingerprint string
	}
	existingByKey := make(map[string]existing, len(existingCmds))
	for _, c := range existingCmds {
		key := fmt.Sprintf("%s:%d", c.Name(), c.Type())
		existingByKey[key] = existing{
			id:          c.ID(),
			name:        c.Name(),
			commandType: c.Type(),
			fingerprint: fingerprint(fromWire(c)),
		}
	}
	desiredByKey := make(map[string]*cmdadapter.SlashCommand, len(desired))
	for _, c := range desired {
		desiredByKey[fmt.Sprintf("%s:%d", c.Name, commandType(c.Type))] = c
	}

	var created, edited, deleted, unchanged int

	m.log.Info().Str("guild_id", guildID).Int("desired", len(desired)).
		Int("existing", len(existingCmds)).Msg("commands_sync_start")

	for key, want := range desiredByKey {
		if have, ok := existingByKey[key]; ok {
			if have.fingerprint == fingerprint(want) {
				unchanged++
				continue
			}
			update := reply.SlashCommandUpdate(want)
			if update == nil {
				continue
			}
			if _, err := m.client.Rest.UpdateGuildCommand(appID, gid, have.id, update); err != nil {
				m.log.Error().Str("guild_id", guildID).Str("command", want.Name).
					Err(err).Msg("command_edit_failed")
			} else {
				edited++
			}
			continue
		}

		create := reply.SlashCommandCreate(want)
		if create == nil {
			continue
		}
		if _, err := m.client.Rest.CreateGuildCommand(appID, gid, create); err != nil {
			m.log.Error().Str("guild_id", guildID).Str("command", want.Name).
				Err(err).Msg("command_create_failed")
		} else {
			created++
		}
	}

	for key, have := range existingByKey {
		if _, ok := desiredByKey[key]; ok {
			continue
		}
		if err := m.client.Rest.DeleteGuildCommand(appID, gid, have.id); err != nil {
			m.log.Error().Str("guild_id", guildID).Str("command", have.name).
				Err(err).Msg("command_delete_failed")
		} else {
			deleted++
		}
	}

	m.log.Info().Str("guild_id", guildID).Int("created", created).Int("edited", edited).
		Int("deleted", deleted).Int("unchanged", unchanged).Msg("commands_sync_done")

	return nil
}

func (m *Syncer) guildLock(guildID string) *sync.Mutex {
	v, _ := m.perGuildLocks.LoadOrStore(guildID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// buildCommandDefinitions collects what the registry declares, in neutral
// form. The rendering to disgo's types happens at the point of registration.
func (m *Syncer) buildCommandDefinitions() []*cmdadapter.SlashCommand {
	var defs []*cmdadapter.SlashCommand
	for _, c := range m.registry.GetAll() {
		if def := declarationOf(c); def != nil {
			defs = append(defs, def)
		}
	}
	return defs
}

// declarationOf resolves a registered command to what it declares.
//
// The assertions are what broke once already: middleware unwraps to the
// Adapter, and an Adapter whose method returns a different type than the
// interface declares simply is not one, with no compile error to say so. A
// command that resolves to nothing here is a command this backend would
// delete from the guild.
func declarationOf(c command.Command) *cmdadapter.SlashCommand {
	root := command.Root(c)

	if slash, ok := root.(cmdadapter.SlashProvider); ok {
		if def := slash.SlashDefinition(); def != nil {
			return def
		}
	}

	return nil
}

func commandType(t cmdadapter.SlashCommandType) discord.ApplicationCommandType {
	switch t {
	case cmdadapter.MessageMenuCommand:
		return discord.ApplicationCommandTypeMessage
	case cmdadapter.UserMenuCommand:
		return discord.ApplicationCommandTypeUser
	default:
		return discord.ApplicationCommandTypeSlash
	}
}
