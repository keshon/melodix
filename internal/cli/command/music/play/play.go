// Package play is the CLI's play: link, search query or history id(s).
package play

import (
	"errors"
	"strings"

	"github.com/keshon/melodix/internal/cli"
	"github.com/keshon/melodix/internal/cli/command/music/playback"
	"github.com/keshon/melodix/internal/music"
)

type Command struct{}

func (Command) Name() string        { return "play" }
func (Command) Aliases() []string   { return []string{"p"} }
func (Command) Description() string { return "Play a music track" }
func (Command) Category() string    { return "🎵 Music" }
func (Command) Usage() string {
	return "<link|query|history ids> [source=youtube|soundcloud|radio] [parser=…]"
}

func (Command) Run(c *cli.Context, args []string) error {
	words, opts := cli.Options(args, "source", "parser")
	if len(words) == 0 {
		c.Println("Usage: play", Command{}.Usage())
		return nil
	}
	in, err := music.ParseInput(strings.Join(words, " "))
	if errors.Is(err, music.ErrTooManyItems) {
		c.Println("Too many tracks in one command.")
		return nil
	}
	if err != nil {
		c.Println("Invalid input:", err)
		return nil
	}

	added, err := c.Music.Add(c.Scope, in, opts["source"], opts["parser"])
	if err != nil {
		playback.AddError(c, err)
		return nil
	}
	playback.Start(c, added)
	return nil
}
