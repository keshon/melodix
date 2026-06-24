package help

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

func runHelpByCategory() string {
	all := command.DefaultRegistry.GetAll()

	categoryMap := make(map[string][]command.Command)
	categorySort := make(map[string]int)

	for _, c := range all {
		meta, _ := command.Root(c).(cmdadapter.Meta)
		cat := ""
		if meta != nil {
			cat = meta.Category()
		}
		categoryMap[cat] = append(categoryMap[cat], c)
		if _, ok := categorySort[cat]; !ok {
			categorySort[cat] = config.CategoryWeights[cat]
		}
	}

	type catSort struct {
		Name string
		Sort int
	}
	var sortedCats []catSort
	for cat, sortVal := range categorySort {
		sortedCats = append(sortedCats, catSort{cat, sortVal})
	}
	sort.Slice(sortedCats, func(i, j int) bool {
		return sortedCats[i].Sort < sortedCats[j].Sort
	})

	var sb strings.Builder
	for _, cat := range sortedCats {
		fmt.Fprintf(&sb, "**%s**\n", cat.Name)
		cmds := categoryMap[cat.Name]
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
		for _, c := range cmds {
			fmt.Fprintf(&sb, "`%s` - %s\n", c.Name(), c.Description())
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
