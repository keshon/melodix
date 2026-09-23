package commands

import (
	"sort"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/adapter"
)

const (
	discordMaxMessageLength = 2000
	codeLeftBlockWrapper    = "```md"
	codeRightBlockWrapper   = "```"
)

var maxContentLength = discordMaxMessageLength - len(codeLeftBlockWrapper) - len(codeRightBlockWrapper)

// CommandsSubcommandOptions returns slash options for command management.
func CommandsSubcommandOptions() []adapter.SlashOption {
	groupChoices := groupOptionChoices()

	return []adapter.SlashOption{
		{
			Type:        adapter.OptionSubCommand,
			Name:        "log",
			Description: "Review recently used commands",
		},
		{
			Type:        adapter.OptionSubCommand,
			Name:        "status",
			Description: "Show enabled and disabled command groups",
		},
		{
			Type:        adapter.OptionSubCommand,
			Name:        "enable",
			Description: "Enable a command group",
			Options: []adapter.SlashOption{
				{
					Type:        adapter.OptionString,
					Name:        "group",
					Description: "Choose command group to enable",
					Required:    true,
					Choices:     groupChoices,
				},
			},
		},
		{
			Type:        adapter.OptionSubCommand,
			Name:        "disable",
			Description: "Disable a command group",
			Options: []adapter.SlashOption{
				{
					Type:        adapter.OptionString,
					Name:        "group",
					Description: "Choose command group to disable",
					Required:    true,
					Choices:     groupChoices,
				},
			},
		},
	}
}

func groupOptionChoices() []adapter.SlashChoice {
	groupChoices := []adapter.SlashChoice{}
	for _, g := range GetUniqueGroups() {
		groupChoices = append(groupChoices, adapter.SlashChoice{Name: g, Value: g})
	}
	sort.Slice(groupChoices, func(i, j int) bool { return groupChoices[i].Name < groupChoices[j].Name })
	return groupChoices
}

// GetUniqueGroups returns sorted command group names from the registry.
func GetUniqueGroups() []string {
	set := map[string]struct{}{}
	for _, c := range command.DefaultRegistry.GetAll() {
		meta, _ := command.Root(c).(adapter.Meta)
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
