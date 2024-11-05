package player

import (
	"github.com/bwmarrin/discordgo"
	"github.com/keshon/dca"
	"github.com/keshon/melodix/song"
)

type Player struct {
	Status          Status
	Song            *song.Song
	Queue           []*song.Song
	ChannelID       string
	GuildID         string
	DiscordSession  *discordgo.Session
	streamSession   *dca.StreamingSession
	encodingSession *dca.EncodeSession
}

func NewPlayer() *Player {
	return &Player{}
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
