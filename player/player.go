package player

import (
	"fmt"
	"io"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/dca"
	"github.com/keshon/melodix/song"
)

type Player struct {
	GuildID   string
	ChannelID string
	Session   *discordgo.Session
	Status    Status
	Queue     []*song.Song
}

func New(ds *discordgo.Session) *Player {
	return &Player{
		Session: ds,
	}
}

type Status int32

const (
	StatusResting Status = iota
	StatusPlaying
	StatusPaused
	StatusError
)

func (status Status) String() string {
	m := map[Status]string{
		StatusResting: "Resting",
		StatusPlaying: "Playing",
		StatusPaused:  "Paused",
		StatusError:   "Error",
	}

	return m[status]
}

func (status Status) StringEmoji() string {
	m := map[Status]string{
		StatusResting: "💤",
		StatusPlaying: "▶️",
		StatusPaused:  "⏸",
		StatusError:   "❌",
	}

	return m[status]
}

func (p *Player) Play(song *song.Song, startAt time.Duration) {
	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 64
	options.Application = "lowdelay"
	options.FrameDuration = 20
	options.BufferedFrames = 200
	options.CompressionLevel = 10
	options.VBR = true
	options.Volume = 1.0

	encodingSession, err := dca.EncodeFile(song.StreamURL, options)
	if err != nil {
		// Handle the error
	}
	defer encodingSession.Cleanup()

	vc, err := p.joinVoiceChannel(p.Session, p.GuildID, p.ChannelID)
	if err != nil {
		// Handle the error
	}
	defer vc.Disconnect()

	done := make(chan error)
	dca.NewStream(encodingSession, vc, done)
	err = <-done
	if err != nil && err != io.EOF {
		// Handle the error
	}
}

func (p *Player) joinVoiceChannel(session *discordgo.Session, guildID, channelID string) (*discordgo.VoiceConnection, error) {
	var voiceConnection *discordgo.VoiceConnection
	var err error

	session.ShouldReconnectOnError = true

	for attempts := 0; attempts < 5; attempts++ {
		voiceConnection, err = session.ChannelVoiceJoin(guildID, channelID, false, false)
		if err == nil {
			return voiceConnection, nil
		}

		time.Sleep(300 * time.Millisecond)
	}

	return nil, fmt.Errorf("failed to join voice channel %s in guild %s after multiple attempts: %w", channelID, guildID, err)
}
