package help

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

func runHelpByGroup() string {
	all := command.DefaultRegistry.GetAll()

	groupMap := make(map[string][]command.Command)
	for _, c := range all {
		meta, _ := command.Root(c).(cmdadapter.Meta)
		group := ""
		if meta != nil {
			group = meta.Group()
		}
		groupMap[group] = append(groupMap[group], c)
	}

	var sortedGroups []string
	for group := range groupMap {
		sortedGroups = append(sortedGroups, group)
	}
	sort.Strings(sortedGroups)

	var sb strings.Builder
	for _, group := range sortedGroups {
		fmt.Fprintf(&sb, "**%s**\n", group)
		cmds := groupMap[group]
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
		for _, c := range cmds {
			fmt.Fprintf(&sb, "`%s` - %s\n", c.Name(), c.Description())
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
