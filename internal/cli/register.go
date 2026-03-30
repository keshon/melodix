package cli

import (
	"github.com/keshon/commandkit"
	clicore "github.com/keshon/melodix/internal/cli/command/core"
	climusic "github.com/keshon/melodix/internal/cli/command/music"
)

// RegisterCLICommands registers all REPL verbs on the given registry (mirrors discord/command layout: music + core).
func RegisterCLICommands(reg *commandkit.Registry) {
	climusic.Register(reg)
	clicore.Register(reg)
}
