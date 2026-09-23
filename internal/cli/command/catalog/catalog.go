// Package catalog is the CLI's command list, the counterpart of the bot's:
// every command in one place, for the binary to serve and tooling to describe.
package catalog

import (
	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/core/about"
	"github.com/keshon/melodix/internal/cli/command/core/help"
	"github.com/keshon/melodix/internal/cli/command/music/history"
	"github.com/keshon/melodix/internal/cli/command/music/next"
	"github.com/keshon/melodix/internal/cli/command/music/play"
	"github.com/keshon/melodix/internal/cli/command/music/queue"
	"github.com/keshon/melodix/internal/cli/command/music/search"
	"github.com/keshon/melodix/internal/cli/command/music/stop"
)

// Register adds every command to reg.
func Register(reg *cli.Registry) {
	reg.Register(about.Command{})
	reg.Register(help.Command{})
	reg.Register(play.Command{})
	reg.Register(search.Command{})
	reg.Register(next.Command{})
	reg.Register(queue.Command{})
	reg.Register(stop.Command{})
	reg.Register(history.Command{})
}
