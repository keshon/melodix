package help

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/commandkit"
)

func runHelpFlat() string {
	all := commandkit.DefaultRegistry.GetAll()
	sort.Slice(all, func(i, j int) bool { return all[i].Name() < all[j].Name() })

	var sb strings.Builder
	for _, c := range all {
		sb.WriteString(fmt.Sprintf("`%s` - %s\n", c.Name(), c.Description()))
	}
	return sb.String()
}
