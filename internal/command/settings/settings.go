package settings

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/melodix/internal/command/core/commands"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
	"github.com/keshon/melodix/internal/discord/discordreply"

	"github.com/keshon/melodix/internal/storage"
)

type SettingsCommand struct{}

func (c *SettingsCommand) Name() string        { return "settings" }
func (c *SettingsCommand) Description() string { return "Server settings" }
func (c *SettingsCommand) Group() string       { return "core" }
func (c *SettingsCommand) Category() string    { return "⚙️ Settings" }
func (c *SettingsCommand) UserPermissions() []int64 {
	return []int64{discordgo.PermissionAdministrator}
}

func (c *SettingsCommand) SlashDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
				Name:        "commands",
				Description: "Command group management",
				Options:     commands.CommandsSubcommandOptions(),
			},
		},
	}
}

func (c *SettingsCommand) Run(ctx interface{}) error {
	context, ok := ctx.(*cmdadapter.SlashInteractionContext)
	if !ok {
		return nil
	}

	s := context.Session
	e := context.Event
	st := context.Storage

	data := e.ApplicationCommandData()
	if len(data.Options) == 0 {
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No settings group provided.",
		})
	}

	group := data.Options[0]
	if len(group.Options) == 0 {
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: "No subcommand provided.",
		})
	}

	sub := group.Options[0]

	switch group.Name {
	case "commands":
		return runCommandsSettings(s, e, *st, context.Syncer, sub)
	default:
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown settings group: %s", group.Name),
		})
	}
}

func runCommandsSettings(s *discordgo.Session, e *discordgo.InteractionCreate, st storage.Storage, syncer cmdadapter.CommandSyncer, sub *discordgo.ApplicationCommandInteractionDataOption) error {
	switch sub.Name {
	case "log":
		return commands.RunCmdLog(s, e, st)
	case "status":
		return commands.RunCmdStatus(s, e, st)
	case "enable":
		return commands.RunCmdEnable(s, e, st, syncer, sub)
	case "disable":
		return commands.RunCmdDisable(s, e, st, syncer, sub)
	default:
		return discordreply.RespondEmbedEphemeral(s, e, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}
