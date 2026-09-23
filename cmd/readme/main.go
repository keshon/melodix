// cmd/readme regenerates README.md from both frontends' command lists. Run it
// from the repo root: go run ./cmd/readme
package main

import (
	"os"

	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/cli"
	clicatalog "github.com/keshon/melodix/internal/cli/command/catalog"
	"github.com/keshon/melodix/internal/config"
	botcatalog "github.com/keshon/melodix/internal/discord/command/catalog"
	"github.com/keshon/melodix/internal/readme"
)

func main() {
	log := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// The bot's commands are only described here, never run, so they get no
	// bot to run against.
	botcatalog.Register(nil, log)
	terminal := cli.NewRegistry()
	clicatalog.Register(terminal)

	if err := readme.Generate(command.DefaultRegistry, terminal, config.CategoryWeights, log); err != nil {
		log.Error().Err(err).Msg("readme_update_failed")
		os.Exit(1)
	}
}
