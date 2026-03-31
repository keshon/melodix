package help

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/command"
)

func runHelpByGroup() string {
	all := commandkit.DefaultRegistry.GetAll()

	groupMap := make(map[string][]commandkit.Command)
	for _, c := range all {
		meta, _ := commandkit.Root(c).(command.Meta)
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
		sb.WriteString(fmt.Sprintf("**%s**\n", group))
		cmds := groupMap[group]
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
		for _, c := range cmds {
			sb.WriteString(fmt.Sprintf("`%s` - %s\n", c.Name(), c.Description()))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
