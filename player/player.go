package player

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/keshon/dca"
	"github.com/keshon/melodix/song"
)

type Player struct {
	Status         Status
	Song           *song.Song
	Queue          []*song.Song
	ChannelID      string
	GuildID        string
	DiscordSession *discordgo.Session
}

func New(ds *discordgo.Session) *Player {
	return &Player{DiscordSession: ds}
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

func (p *Player) Play(channelID string, guildID string, url string) {
	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 96
	options.Application = "lowdelay"

	encodingSession, err := dca.EncodeFile(url, options)
	if err != nil {
		// Handle the error
	}
	defer encodingSession.Cleanup()

	vc, err := p.setupVoiceConnection(guildID, channelID)
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

func (p *Player) setupVoiceConnection(guildID, channelID string) (*discordgo.VoiceConnection, error) {
	// Helpful: https://github.com/bwmarrin/discordgo/issues/1357
	session := p.DiscordSession

	var vc *discordgo.VoiceConnection
	var err error

	session.ShouldReconnectOnError = true

	for attempts := 0; attempts < 5; attempts++ {
		vc, err = session.ChannelVoiceJoin(guildID, channelID, false, false)
		if err == nil {
			break
		}

		if attempts > 0 {
			slog.Warn("Failed to join voice channel after multiple attempts, attempting to disconnect and reconnect next iteration")
			if vc != nil {
				vc.Disconnect()
			}
		}

		slog.Warn("Failed to join voice channel (attempt %d): %v", attempts+1, err)
		time.Sleep(300 * time.Millisecond)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel after multiple attempts: %w", err)
	}

	slog.Info("Successfully joined voice channel")
	return vc, nil
}
