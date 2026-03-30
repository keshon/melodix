package music

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/keshon/melodix/internal/cli/command"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/playinput"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolver"
)

// ParseHistoryArgs parses optional view and page for the history command.
func ParseHistoryArgs(args []string) (view string, page int64) {
	view = "timeline"
	page = 1
	switch len(args) {
	case 1:
		a0 := strings.ToLower(strings.TrimSpace(args[0]))
		if a0 == "timeline" || a0 == "counts" {
			view = a0
		} else if pg, err := strconv.ParseInt(a0, 10, 64); err == nil && pg >= 1 {
			page = pg
		}
	case 2:
		a0 := strings.ToLower(strings.TrimSpace(args[0]))
		if a0 == "timeline" || a0 == "counts" {
			view = a0
			if pg, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64); err == nil && pg >= 1 {
				page = pg
			}
		}
	}
	return view, page
}

// PlayFromArgs parses CLI play arguments and enqueues tracks via the music facade.
func PlayFromArgs(m *musicapp.Music, p *player.Player, guildScope string, res *resolver.SourceResolver, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no input")
	}

	if len(args) >= 2 && command.AllUintStringTokens(args) {
		parsed, err := playinput.ParsePlayInput(strings.Join(args, " "))
		if err != nil {
			return err
		}
		if parsed.Kind == playinput.PlayInputKindHistoryIDs {
			return m.EnqueueFromParsedInput(p, guildScope, parsed, "", "", res.Resolve, musicapp.QueryViaPlayerEnqueue)
		}
	}

	parsed, err := playinput.ParsePlayInput(args[0])
	if err != nil {
		return err
	}
	if parsed.Kind == playinput.PlayInputKindHistoryIDs {
		return m.EnqueueFromParsedInput(p, guildScope, parsed, "", "", res.Resolve, musicapp.QueryViaPlayerEnqueue)
	}

	source, parser := "", ""
	if len(args) > 1 {
		source = args[1]
	}
	if len(args) > 2 {
		parser = args[2]
	}
	return m.EnqueueFromParsedInput(p, guildScope, parsed, source, parser, res.Resolve, musicapp.QueryViaPlayerEnqueue)
}
