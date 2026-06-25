package help

import (
	"sort"
	"strings"

	"github.com/keshon/command"
	"github.com/keshon/melodix/internal/discord/cmdadapter"
)

func runHelpFlat() string {
	all := command.DefaultRegistry.GetAll()
	sort.Slice(all, func(i, j int) bool { return all[i].Name() < all[j].Name() })

	var sb strings.Builder
	for _, c := range all {
		sb.WriteString(cmdadapter.FormatCommandWithSubcommands(c))
	}
	return sb.String()
}
