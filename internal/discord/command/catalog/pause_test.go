package catalog

import (
	"strings"
	"testing"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/discord"
	"github.com/keshon/melodix/internal/discord/adapter"
	"github.com/keshon/melodix/internal/discord/command/music/pause"
	"github.com/keshon/melodix/internal/discord/command/music/resume"
	"github.com/keshon/melodix/internal/music"
	"github.com/keshon/melodix/pkg/music/opus"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
)

type pauseBot struct {
	discord.VoiceAPI
	music   *music.Service
	channel string
}

func (b *pauseBot) Music() *music.Service { return b.music }
func (b *pauseBot) FindUserVoiceState(string, string) (*discord.UserVoiceState, error) {
	return &discord.UserVoiceState{ChannelID: b.channel}, nil
}

type pauseReplies struct {
	adapter.Responder
	last adapter.Reply
}

func (r *pauseReplies) AckDeferred(bool) error { return nil }
func (r *pauseReplies) Followup(reply adapter.Reply) error {
	r.last = reply
	return nil
}

type pauseOutput struct{}

func (pauseOutput) Sink(string) (sink.AudioSink, error) { return pauseOutput{}, nil }
func (pauseOutput) ReleaseSink(string)                  {}
func (pauseOutput) InvalidateSink()                     {}
func (pauseOutput) Stream(_ opus.Reader, control *sink.Control) error {
	<-control.Stop
	return stream.ErrPlaybackStopped
}

type pauseAudio struct{}

func (pauseAudio) ReadPacket() ([]byte, error) { return []byte{1}, nil }
func (pauseAudio) Close() error                { return nil }
func (pauseAudio) Open(parsers.Track, float64) (parsers.Opened, error) {
	return parsers.Opened{Reader: pauseAudio{}, Cleanup: func() {}}, nil
}

func TestPauseAndResumeRequireThePlaybackChannel(t *testing.T) {
	previous := stream.SetRegistry(map[string]parsers.Streamer{"test": pauseAudio{}})
	t.Cleanup(func() { stream.SetRegistry(previous) })
	svc := music.New(&config.Config{}, nil, zerolog.Nop(), music.Hooks{
		NewSink: func(string) sink.Provider { return pauseOutput{} },
	})
	t.Cleanup(svc.StopAll)
	p := svc.Player("guild")
	err := p.EnqueueTrackInfos([]sources.TrackInfo{{
		URL: "https://example.com/track", Title: "Track", AvailableParsers: []string{"test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = p.PlayNext("voice")
	if err != nil {
		t.Fatal(err)
	}
	bot := &pauseBot{music: svc, channel: "other"}
	replies := &pauseReplies{}
	ctx := &adapter.SlashInteractionContext{
		Invoker:   adapter.Invoker{GuildID: "guild", UserID: "listener"},
		Responder: replies,
		AppLog:    zerolog.Nop(),
	}
	pausing := &pause.Command{Bot: bot}
	resuming := &resume.Command{Bot: bot}
	err = pausing.Run(ctx)
	if err != nil || p.IsPaused() || !replies.last.Ephemeral {
		t.Fatalf("another channel could pause playback: %v", err)
	}

	bot.channel = "voice"
	err = pausing.Run(ctx)
	if err != nil || !p.IsPaused() || !strings.Contains(replies.last.Embed.Description, "Playback paused") {
		t.Fatalf("pause did not succeed: %v", err)
	}
	bot.channel = "other"
	err = resuming.Run(ctx)
	if err != nil || !p.IsPaused() || !replies.last.Ephemeral {
		t.Fatalf("another channel could resume playback: %v", err)
	}
	bot.channel = "voice"
	err = resuming.Run(ctx)
	if err != nil || p.IsPaused() || !strings.Contains(replies.last.Embed.Description, "Playback resumed") {
		t.Fatalf("resume did not succeed: %v", err)
	}
}
