package music

import (
	"context"
	"errors"
	"fmt"
	"io"

	clicommand "github.com/keshon/melodix/internal/cli/command"
	"github.com/keshon/commandkit"
	"github.com/keshon/melodix/internal/musicapp"
	"github.com/keshon/melodix/internal/playinput"
	"github.com/keshon/melodix/pkg/music/player"
)

// Register adds music-related REPL verbs to the registry.
func Register(reg *commandkit.Registry) {
	for _, c := range []clicommand.CLICommand{
		cmdPlay{name: "play", desc: "enqueue or play from history ids"},
		cmdPlay{name: "p", desc: "enqueue or play from history ids (short)"},
		cmdNext{},
		cmdNextAlias{name: "n"},
		cmdNextAlias{name: "skip"},
		cmdStop{},
		cmdStopAlias{},
		cmdQueue{},
		cmdStatus{},
		cmdHistory{name: "history", desc: "playback history"},
		cmdHistory{name: "h", desc: "playback history (short)"},
	} {
		clicommand.Register(reg, c)
	}
}

type cmdPlay struct {
	name, desc string
}

func (c cmdPlay) Name() string        { return c.name }
func (c cmdPlay) Description() string { return c.desc }

func (c cmdPlay) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(d.Out, "Usage: play <url|query|id> [source] [parser]")
		return nil
	}
	return RunPlayDispatch(d, args)
}

type cmdNext struct{}

func (cmdNext) Name() string        { return "next" }
func (cmdNext) Description() string { return "play next in queue" }

func (cmdNext) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	return runNext(d.Out, d.Player)
}

type cmdNextAlias struct{ name string }

func (c cmdNextAlias) Name() string        { return c.name }
func (c cmdNextAlias) Description() string { return "play next in queue (alias)" }

func (c cmdNextAlias) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	return runNext(d.Out, d.Player)
}

func runNext(w io.Writer, p *player.Player) error {
	if p.IsPlaying() {
		_ = p.Stop(false)
	}
	if err := p.PlayNext(""); err != nil {
		if err == player.ErrNoTracksInQueue {
			fmt.Fprintln(w, "Queue is empty")
			return nil
		}
		return err
	}
	return nil
}

type cmdStop struct{}

func (cmdStop) Name() string        { return "stop" }
func (cmdStop) Description() string { return "stop playback and clear queue" }

func (cmdStop) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	_ = d.Player.Stop(true)
	fmt.Fprintln(d.Out, "stopped")
	return nil
}

type cmdStopAlias struct{}

func (cmdStopAlias) Name() string        { return "s" }
func (cmdStopAlias) Description() string { return "stop playback (short)" }

func (cmdStopAlias) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	_ = d.Player.Stop(true)
	fmt.Fprintln(d.Out, "stopped")
	return nil
}

type cmdQueue struct{}

func (cmdQueue) Name() string        { return "queue" }
func (cmdQueue) Description() string { return "show queue" }

func (cmdQueue) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	cur := d.Player.CurrentTrack()
	if cur != nil {
		fmt.Fprintln(d.Out, "Now playing:", cur.DisplayLabel())
	}
	for i, t := range d.Player.Queue() {
		fmt.Fprintf(d.Out, "  %d. %s\n", i+1, t.DisplayLabel())
	}
	if cur == nil && len(d.Player.Queue()) == 0 {
		fmt.Fprintln(d.Out, "(empty)")
	}
	return nil
}

type cmdStatus struct{}

func (cmdStatus) Name() string        { return "status" }
func (cmdStatus) Description() string { return "playing / queue length" }

func (cmdStatus) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	if cur := d.Player.CurrentTrack(); cur != nil {
		fmt.Fprintln(d.Out, "Playing:", cur.DisplayLabel(), "| Queue:", len(d.Player.Queue()))
	} else {
		fmt.Fprintln(d.Out, "Stopped. Queue:", len(d.Player.Queue()))
	}
	return nil
}

type cmdHistory struct {
	name, desc string
}

func (c cmdHistory) Name() string        { return c.name }
func (c cmdHistory) Description() string { return c.desc }

func (c cmdHistory) Run(ctx context.Context, d *clicommand.Data, args []string) error {
	view, page := ParseHistoryArgs(args)
	res, err := d.Music.BuildHistoryPage(d.GuildScope, page, view)
	if errors.Is(err, musicapp.ErrHistoryEmpty) {
		fmt.Fprintln(d.Out, MsgHistoryEmpty)
		return nil
	}
	if err != nil {
		return err
	}
	PrintHistoryPage(d.Out, res)
	return nil
}

// RunPlayDispatch runs the play path after a successful parse, including starting playback if idle.
func RunPlayDispatch(d *clicommand.Data, args []string) error {
	if err := PlayFromArgs(d.Music, d.Player, d.GuildScope, d.Resolver, args); err != nil {
		if errors.Is(err, playinput.ErrPlayInputTooManyItems) {
			fmt.Fprintln(d.Out, "Error: too many items in one command (max", playinput.MaxPlayBatchItems, ")")
			return nil
		}
		fmt.Fprintln(d.Out, "Error:", err)
		return nil
	}
	if !d.Player.IsPlaying() {
		if err := d.Player.PlayNext(""); err != nil && err != player.ErrNoTracksInQueue {
			fmt.Fprintln(d.Out, "Play error:", err)
		}
	}
	return nil
}
