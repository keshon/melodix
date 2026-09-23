// Package catalog is the bot's command list: every command it offers, in one
// place, with the middleware each runs through. The binary registers it to
// serve commands, and tooling registers it to describe them.
package catalog

import (
	"github.com/keshon/command"
	"github.com/rs/zerolog"

	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/command/core/about"
	"github.com/keshon/melodix/internal/discord/command/core/help"
	"github.com/keshon/melodix/internal/discord/command/core/maintenance"
	"github.com/keshon/melodix/internal/discord/command/music/history"
	"github.com/keshon/melodix/internal/discord/command/music/next"
	"github.com/keshon/melodix/internal/discord/command/music/play"
	"github.com/keshon/melodix/internal/discord/command/music/queue"
	"github.com/keshon/melodix/internal/discord/command/music/search"
	"github.com/keshon/melodix/internal/discord/command/music/stop"
	"github.com/keshon/melodix/internal/discord/command/settings"
	"github.com/keshon/melodix/internal/discord/middleware"
)

// Register adds every command to command.DefaultRegistry. bot may be nil when
// the commands are only being described, never run.
func Register(bot *discord.Bot, log zerolog.Logger) {
	mw := defaultMiddleware(log)
	adapter.Register(&settings.Command{}, mw...)
	adapter.Register(&about.Command{}, mw...)
	adapter.Register(&help.Command{}, mw...)
	adapter.Register(&maintenance.Command{}, mw...)
	adapter.Register(&play.Command{Bot: bot}, mw...)
	adapter.Register(&search.Command{Bot: bot}, mw...)
	adapter.Register(&next.Command{Bot: bot}, mw...)
	adapter.Register(&queue.Command{Bot: bot}, mw...)
	adapter.Register(&stop.Command{Bot: bot}, mw...)
	adapter.Register(&history.Command{Bot: bot}, mw...)
}

func defaultMiddleware(log zerolog.Logger) []command.Middleware {
	return []command.Middleware{
		middleware.WithGroupAccessCheck(),
		middleware.WithGuildOnly(),
		middleware.WithUserPermissionCheck(),
		middleware.WithCommandLogger(log),
	}
}
