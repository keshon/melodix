package commands

import (
	"sort"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"

	"github.com/bwmarrin/discordgo"
)

const (
	discordMaxMessageLength = 2000
	codeLeftBlockWrapper    = "```md"
	codeRightBlockWrapper   = "```"
)

var maxContentLength = discordMaxMessageLength - len(codeLeftBlockWrapper) - len(codeRightBlockWrapper)

// CommandsSubcommandOptions returns slash options for command management.
func CommandsSubcommandOptions() []*discordgo.ApplicationCommandOption {
	groupChoices := groupOptionChoices()

	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "log",
			Description: "Review recently used commands",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "status",
			Description: "Show enabled and disabled command groups",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "enable",
			Description: "Enable a command group",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "group",
					Description: "Choose command group to enable",
					Required:    true,
					Choices:     groupChoices,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "disable",
			Description: "Disable a command group",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "group",
					Description: "Choose command group to disable",
					Required:    true,
					Choices:     groupChoices,
				},
			},
		},
	}
}

func groupOptionChoices() []*discordgo.ApplicationCommandOptionChoice {
	groupChoices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, g := range GetUniqueGroups() {
		groupChoices = append(groupChoices, &discordgo.ApplicationCommandOptionChoice{Name: g, Value: g})
	}
	sort.Slice(groupChoices, func(i, j int) bool { return groupChoices[i].Name < groupChoices[j].Name })
	return groupChoices
}

// GetUniqueGroups returns sorted command group names from the registry.
func GetUniqueGroups() []string {
	set := map[string]struct{}{}
	for _, c := range command.DefaultRegistry.GetAll() {
		meta, _ := command.Root(c).(cmdadapter.Meta)
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

// getUniqueGroups is kept for internal callers within the package.
func getUniqueGroups() []string {
	return GetUniqueGroups()
}
