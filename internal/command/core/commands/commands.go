package commands

import (
	"fmt"
	"sort"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
	"github.com/keshon/melodix/internal/discord/discordreply"

	"github.com/bwmarrin/discordgo"
)

type Commands struct{}

func (c *Commands) Name() string        { return "commands" }
func (c *Commands) Description() string { return "Manage or inspect commands" }
func (c *Commands) Group() string       { return "core" }
func (c *Commands) Category() string    { return "⚙️ Settings" }
func (c *Commands) UserPermissions() []int64 {
	return []int64{discordgo.PermissionAdministrator}
}

const (
	discordMaxMessageLength = 2000
	codeLeftBlockWrapper    = "```md"
	codeRightBlockWrapper   = "```"
)

var maxContentLength = discordMaxMessageLength - len(codeLeftBlockWrapper) - len(codeRightBlockWrapper)

func (c *Commands) SlashDefinition() *discordgo.ApplicationCommand {
	groupChoices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, g := range getUniqueGroups() {
		groupChoices = append(groupChoices, &discordgo.ApplicationCommandOptionChoice{Name: g, Value: g})
	}
	sort.Slice(groupChoices, func(i, j int) bool { return groupChoices[i].Name < groupChoices[j].Name })

	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: c.Description(),
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "log",
				Description: "Review recent commands called by users",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Check which command groups are enabled or disabled",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "toggle",
				Description: "Enable or disable a group of commands",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "group",
						Description: "Choose command group to toggle",
						Required:    true,
						Choices:     groupChoices,
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "state",
						Description: "Enable or disable",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Enable", Value: "enable"},
							{Name: "Disable", Value: "disable"},
						},
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "update",
				Description: "Re-register or update slash commands",
			},
		},
	}
}

func (c *Commands) Run(ctx interface{}) error {
	context, ok := ctx.(*command.SlashInteractionContext)
	if !ok {
		return nil
	}

	session := context.Session
	event := context.Event
	storage := context.Storage

	if len(event.ApplicationCommandData().Options) == 0 {
		return nil
	}

	sub := event.ApplicationCommandData().Options[0]

	switch sub.Name {
	case "log":
		return c.runCmdLog(session, event, *storage)
	case "status":
		return c.runCmdStatus(session, event, *storage)
	case "toggle":
		return c.runCmdToggle(session, event, *storage, context.Syncer)
	case "update":
		return c.runCmdUpdate(session, event, context.Syncer)
	default:
		return discordreply.RespondEmbedEphemeral(session, event, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Unknown subcommand: %s", sub.Name),
		})
	}
}

func getUniqueGroups() []string {
	set := map[string]struct{}{}
	for _, c := range commandkit.DefaultRegistry.GetAll() {
		meta, _ := commandkit.Root(c).(command.Meta)
		group := ""
		if meta != nil {
			group = meta.Group()
		}
		if group != "" {
			set[group] = struct{}{}
		}
	}
	var result []string
	for group := range set {
		result = append(result, group)
	}
	sort.Strings(result)
	return result
}
