package player

import (
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/dca"
	songpkg "github.com/keshon/melodix/song"
)

type Player struct {
	ChannelID string
	GuildID   string
	Session   *discordgo.Session
	Status    Status
	Queue     []*songpkg.Song
	Signals   chan Signal
}

func New(ds *discordgo.Session) *Player {
	return &Player{
		Session: ds,
		Status:  StatusResting,
		Queue:   make([]*songpkg.Song, 0, 10),
		Signals: make(chan Signal, 1),
	}
}

type Status int32
type Signal int32

const (
	StatusResting Status = iota
	StatusPlaying
	StatusPaused
	StatusError

	ActionStop Signal = iota
	ActionSkip
	ActionSwap
	ActionPauseResume
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

func (p *Player) Play(song *songpkg.Song, startAt time.Duration) error {
	if song == nil {
		if len(p.Queue) == 0 {
			return fmt.Errorf("queue is empty")
		}

		song = p.Queue[0]
		p.Queue = p.Queue[1:]
	}

	options := dca.StdEncodeOptions
	options.RawOutput = true
	options.Bitrate = 96
	options.Application = "lowdelay"
	options.FrameDuration = 20
	options.BufferedFrames = 200
	options.CompressionLevel = 10
	options.VBR = true
	options.VolumeFloat = 1.0
	options.StartTime = startAt
	options.EncodingLineLog = true

	encoding, err := dca.EncodeFile(song.StreamURL, options)
	if err != nil {
		return err
	}
	defer encoding.Cleanup()

	vc, err := p.joinVoiceChannel(p.Session, p.GuildID, p.ChannelID)
	if err != nil {
		return err
	}
	defer p.leaveVoiceChannel(vc) // Ensure vc is disconnected at the end

	done := make(chan error)

	streaming := dca.NewStream(encoding, vc, done)
	p.Status = StatusPlaying

	for {
		select {
		case err := <-done:
			if err != nil && err != io.EOF {
				p.leaveVoiceChannel(vc)
				return err
			}

			if song != nil {
				switch song.Source {
				case songpkg.SourceYouTube:
					duration, position, err := p.getPlaybackDuration(encoding, streaming, song)
					if err != nil {
						return err
					}
					if encoding.Stats().Duration.Seconds() > 0 && position.Seconds() > 0 && position < duration {
						fmt.Printf("Unexpected interruption of YouTube playback, restarting: \"%v\" from %vs\n", song.Title, position.Seconds())

						encoding.Stop()
						encoding.Cleanup()
						p.leaveVoiceChannel(vc)

						go func() error {
							song, err := song.GetYoutubeSong(song.PublicLink)
							if err != nil {
								return err
							}
							err = p.Play(song, position)
							if err != nil {
								return err
							}
							return nil
						}()
					} else {
						fmt.Printf("Finished YouTube playback of \"%v\" at %vs of %s", song.Title, int(position.Seconds()), duration)
					}
				case songpkg.SourceInternetRadio:
					fmt.Printf("Unexpected interruption of Internet Radio playback, restarting: \"%v\" from %vs\n", song.Title, float64(startAt))

					encoding.Stop()
					encoding.Cleanup()
					p.leaveVoiceChannel(vc)

					go func() error {
						err = p.Play(song, 0)
						if err != nil {
							return err
						}
						return nil
					}()
				case songpkg.SourceLocalFile:
					duration, position, err := p.getPlaybackDuration(encoding, streaming, song)
					if err != nil {
						return err
					}
					if encoding.Stats().Duration.Seconds() > 0 && position.Seconds() > 0 && position < duration {
						fmt.Printf("Unexpected interruption of local file playback, restarting: \"%v\" from %vs\n", song.Title, position.Seconds())
						startAt := position.Seconds()

						encoding.Stop()
						encoding.Cleanup()
						p.leaveVoiceChannel(vc)

						go func() error {
							err = p.Play(song, time.Duration(startAt)*time.Second)
							if err != nil {
								return err
							}
							return nil
						}()
					}
				}
			}

			return err
		case signal := <-p.Signals:
			switch signal {
			case ActionSkip:
				// Skip to next song (if implemented in your main queue loop)
				return nil
			case ActionStop:
				p.Status = StatusResting
				return p.leaveVoiceChannel(vc)
			case ActionSwap:
				p.Status = StatusResting
				go p.Play(song, 0)
				return nil
			case ActionPauseResume:
				if streaming.Paused() {
					streaming.SetPaused(false)
					p.Status = StatusPlaying
					vc.Speaking(true)
				} else {
					streaming.SetPaused(true)
					p.Status = StatusPaused
					vc.Speaking(false)
				}
			}
		}
	}
}

func (p *Player) joinVoiceChannel(session *discordgo.Session, guildID, channelID string) (*discordgo.VoiceConnection, error) {
	var voiceConnection *discordgo.VoiceConnection
	var err error

	delay := 100 * time.Millisecond

	for attempts := 0; attempts < 5; attempts++ {
		voiceConnection, err = session.ChannelVoiceJoin(guildID, channelID, false, false)
		if err == nil {
			voiceConnection.Speaking(true)
			return voiceConnection, nil
		}
		time.Sleep(delay)
		delay *= 2
	}

	return nil, fmt.Errorf("failed to join voice channel %s in guild %s after multiple attempts: %w", channelID, guildID, err)
}

func (p *Player) leaveVoiceChannel(vc *discordgo.VoiceConnection) error {
	if err := vc.Speaking(false); err != nil {
		return err
	}
	if err := vc.Disconnect(); err != nil {
		return err
	}
	return nil
}

func (p *Player) getPlaybackDuration(encoding *dca.EncodeSession, streaming *dca.StreamingSession, song *songpkg.Song) (time.Duration, time.Duration, error) {
	encodingStartTime := encoding.Options().StartTime
	streamingPos := streaming.PlaybackPosition()
	streamingDelay := encoding.Stats().Duration - streamingPos

	var songDuration time.Duration
	var err error
	switch song.Source {
	case songpkg.SourceYouTube:
		parsedURL, err := url.Parse(song.StreamURL)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse URL: %v", err)
		}
		duration := parsedURL.Query().Get("dur")
		songDuration, err = p.parseDurationFromString(duration)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse duration: %v", err)
		}
	case songpkg.SourceLocalFile:
		songDuration, err = p.fetchMP3Duration(song.StreamFilepath)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse local file duration: %v", err)
		}
	default:
		return 0, 0, fmt.Errorf("unknown source: %v", song.Source)
	}

	playbackPos := encodingStartTime + streamingPos + streamingDelay.Abs() // the delay is wrong here, but I'm out of ideas how to fix the precision

	fmt.Printf("Playback stopped at:\t%s,\tTotal Song duration:\t%s\n", playbackPos, songDuration)
	fmt.Printf("Encoding started at:\t%s,\tStreaming delay:\t%s\n", encodingStartTime, streamingDelay)

	return songDuration, playbackPos, nil
}

func (p *Player) parseDurationFromString(durationStr string) (time.Duration, error) {
	dur, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(dur * float64(time.Second)), nil
}

func (p *Player) fetchMP3Duration(filePath string) (time.Duration, error) {
	cmd := exec.Command("ffmpeg", "-i", filePath, "-f", "null", "-")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffmpeg error: %v", err)
	}

	durationRegex := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2})\.\d+`)
	matches := durationRegex.FindStringSubmatch(string(output))
	if len(matches) != 4 {
		return 0, fmt.Errorf("duration not found in ffmpeg output")
	}

	hours, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(matches[2])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(matches[3])
	if err != nil {
		return 0, err
	}
	totalDuration := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second

	return totalDuration, nil
}
