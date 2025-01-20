package player

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/dca"
	"github.com/keshon/melodix/env"
	"github.com/keshon/melodix/parsers"
	songpkg "github.com/keshon/melodix/song"
	"github.com/keshon/melodix/storage"
)

type Player struct {
	ChannelID     string
	GuildID       string
	Session       *discordgo.Session
	Storage       *storage.Storage
	Song          *songpkg.Song
	Queue         []*songpkg.Song
	StatusSignals chan StatusSignal
	ActionSignals chan ActionSignal
}

func New(ds *discordgo.Session, s *storage.Storage) *Player {
	return &Player{
		Session:       ds,
		Storage:       s,
		Queue:         make([]*songpkg.Song, 0, 10),
		StatusSignals: make(chan StatusSignal, 1),
		ActionSignals: make(chan ActionSignal, 1),
	}
}

type StatusSignal int32
type ActionSignal int32

const (
	StatusPlaying StatusSignal = iota
	StatusResuming
	StatusAdded
	StatusPaused  // reserved, not used
	StatusResting // reserved, not used
	StatusError   // reserved, not used

	ActionStop        ActionSignal = iota // stop the player
	ActionSkip                            // skip the current song
	ActionSwap                            // channel swap
	ActionPauseResume                     // pause or resume
	ActionPlay                            // play
)

func (status StatusSignal) String() string {
	m := map[StatusSignal]string{
		StatusResting:  "Resting",
		StatusPlaying:  "Playing",
		StatusResuming: "Resuming",
		StatusPaused:   "Paused",
		StatusError:    "Error",
	}

	return m[status]
}

func (status StatusSignal) StringEmoji() string {
	m := map[StatusSignal]string{
		StatusResting:  "💤",
		StatusPlaying:  "▶️",
		StatusResuming: "▶️",
		StatusPaused:   "⏸",
		StatusError:    "❌",
	}

	return m[status]
}

var isCached bool

func (p *Player) Play() error {
	var startAt time.Duration = 0
PLAYBACK_LOOP:
	for {
		if startAt == 0 {
			if p.Song == nil {
				if len(p.Queue) == 0 {
					return fmt.Errorf("queue is empty")
				}
				p.Song = p.Queue[0]
				p.Queue = p.Queue[1:]
				isCached = false
			}
		}

		if p.Song == nil {
			return fmt.Errorf("song is empty")
		}

		options := &dca.EncodeOptions{
			Application:             "lowdelay",
			StartTime:               startAt,
			Volume:                  env.GetEnvAsInt("ENCODE_VOLUME", 128),
			Channels:                env.GetEnvAsInt("ENCODE_CHANNELS", 2),
			FrameRate:               env.GetEnvAsInt("ENCODE_FRAME_RATE", 48000),
			FrameDuration:           env.GetEnvAsInt("ENCODE_FRAME_DURATION", 20),
			Bitrate:                 env.GetEnvAsInt("ENCODE_BITRATE", 96),
			CompressionLevel:        env.GetEnvAsInt("ENCODE_COMPRESSION_LEVEL", 10),
			PacketLoss:              env.GetEnvAsInt("ENCODE_PACKET_LOSS", 1),
			BufferedFrames:          env.GetEnvAsInt("ENCODE_BUFFERED_FRAMES", 400),
			VBR:                     env.GetEnvAsBool("ENCODE_VBR", true),
			VolumeFloat:             env.GetEnvAsFloat32("ENCODE_VOLUME_FLOAT", 1.0),
			ReconnectAtEOF:          env.GetEnvAsInt("ENCODE_RECONNECT_AT_EOF", 1),
			ReconnectStreamed:       env.GetEnvAsInt("ENCODE_RECONNECT_STREAMED", 1),
			ReconnectOnNetworkError: env.GetEnvAsInt("ENCODE_RECONNECT_ON_NETWORK_ERROR", 1),
			ReconnectOnHttpError:    env.GetEnv("ENCODE_RECONNECT_ON_HTTP_ERROR", "4xx,5xx"),
			ReconnectDelayMax:       env.GetEnvAsInt("ENCODE_RECONNECT_DELAY_MAX", 5),
			FfmpegBinaryPath:        env.GetEnv("ENCODE_FFMPEG_BINARY_PATH", ""),
			EncodingLineLog:         env.GetEnvAsBool("ENCODE_ENCODING_LINE_LOG", true),
			UserAgent:               env.GetEnv("ENCODE_USER_AGENT", "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.1; SV1)"),
			RawOutput:               env.GetEnvAsBool("ENCODE_RAW_OUTPUT", true),
		}

		streamPath := p.Song.StreamURL

		cachedIsEnabled, err := p.Storage.IsCacheEnabled(p.GuildID)
		if err != nil {
			return err
		}
		cacheReady := make(chan bool)
		if cachedIsEnabled {
			if !isCached {
				go func() {
					cacheDir := "./cache"
					if err := os.MkdirAll(cacheDir, 0755); err != nil {
						fmt.Printf("Error creating cache directory: %v\n", err)
						return
					}

					output, path, err := parsers.NewYtdlpWrapper().DownloadStream(p.Song.PublicLink)
					if err != nil {
						fmt.Printf("Error caching: %v\n", err)
						return
					}

					fmt.Println(output)

					if output.ExitCode == 0 {
						time.Sleep(5 * time.Second)
						if p.Song != nil {
							p.Song.StreamURL = path
							cacheReady <- true
						} else {
							cacheReady <- false
						}
					}

					fmt.Println("Caching is done, switching to playback from cache")
				}()
			}
		}

		time.Sleep(250 * time.Millisecond)
		encoding, err := dca.EncodeFile(streamPath, options)
		if err != nil {
			return err
		}
		defer func() {
			startAt = 0
			encoding.Stop()
			encoding.Cleanup()
			p.Song = nil
			p.Queue = nil
		}()

		vc, err := p.joinVoiceChannel(p.Session, p.GuildID, p.ChannelID)
		if err != nil {
			return err
		}
		defer p.leaveVoiceChannel(vc)

		done := make(chan error)
		// defer close(done)

		streaming := dca.NewStream(encoding, vc, done)
		if startAt == 0 {
			p.StatusSignals <- StatusPlaying
		} else {
			p.StatusSignals <- StatusResuming
		}

		if startAt == 0 {
			hostname, err := extractHostname(p.Song.PublicLink)
			if err != nil {
				hostname = p.Song.Source.String()
			}
			err = p.Storage.AddTrackCountByOne(p.GuildID, p.Song.SongID, p.Song.Title, hostname, p.Song.PublicLink)
			if err != nil {
				return err
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		go func() {
			for {
				select {
				case <-ctx.Done(): // context cancellation signal
					return
				case <-ticker.C:
					if p.Song != nil { // or it will crash on stop signal
						hostname, err := extractHostname(p.Song.PublicLink)
						if err != nil {
							hostname = p.Song.Source.String()
						}
						err = p.Storage.AddTrackDuration(p.GuildID, p.Song.SongID, p.Song.Title, hostname, p.Song.PublicLink, 2*time.Second)
						if err != nil {
							fmt.Printf("Error saving track duration: %v\n", err)
						}
					}
				}
			}
		}()

		for {
			select {
			case doneError := <-done:
				close(done)

				fmt.Println("Playback got done signal")

				// stop if there is an error
				if doneError != nil && doneError != io.EOF {
					p.StatusSignals <- StatusError
					return doneError
				}
				// restart if there is an interrupt
				duration, position, err := p.getPlaybackDuration(encoding, streaming, p.Song)
				if err != nil {
					p.StatusSignals <- StatusError
					return err
				}

				fmt.Printf("Duration: %v, Position: %v\n", duration, position)
				fmt.Printf("Encoding (sec): %v, Position (sec): %v\n", encoding.Stats().Duration.Seconds(), position.Seconds())

				if encoding.Stats().Duration.Seconds() > 0 && position.Seconds() > 0 && position < duration {
					fmt.Printf("Playback interrupted, restarting: \"%v\" from %vs\n", p.Song.Title, position.Seconds())
					encoding.Stop()
					encoding.Cleanup()
					startAt = position
					continue PLAYBACK_LOOP
				}

				if position.Seconds() == 0.0 {
					fmt.Printf("Playback could not be started: \"%v\"\n", p.Song.Title)
					p.StatusSignals <- StatusError
					return fmt.Errorf("playback could not be started: %v", doneError)
				}

				// skip to the next song
				if len(p.Queue) > 0 {
					startAt = 0
					encoding.Stop()
					encoding.Cleanup()
					p.Song = nil
					isCached = false
					continue PLAYBACK_LOOP
				}

				// finished
				fmt.Printf("Finished playback of \"%v\"", p.Song.Title)
				return nil
			case <-cacheReady:
				fmt.Println("Switching to cached playback")

				_, position, err := p.getPlaybackDuration(encoding, streaming, p.Song)
				if err != nil {
					p.StatusSignals <- StatusError
					return err
				}

				isCached = true
				encoding.Cleanup()
				startAt = position
				continue PLAYBACK_LOOP
			case signal := <-p.ActionSignals:
				switch signal {
				case ActionSkip:
					if len(p.Queue) > 0 {
						startAt = 0
						encoding.Stop()
						encoding.Cleanup()
						p.Song = nil
						isCached = false
						continue PLAYBACK_LOOP
					} else {
						p.ActionSignals <- ActionStop
					}
				case ActionStop:
					p.StatusSignals <- StatusResting
					return nil //p.leaveVoiceChannel(vc)
				case ActionSwap:
					encoding.Stop()
					encoding.Cleanup()
					continue PLAYBACK_LOOP
				case ActionPauseResume:
					if streaming.Paused() {
						streaming.SetPaused(false)
						vc.Speaking(true)
					} else {
						streaming.SetPaused(true)
						vc.Speaking(false)
					}
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
	case songpkg.SourcePlatform:
		songDuration = song.Duration
	case songpkg.SourceFile:
		songDuration, err = p.fetchMP3Duration(song.StreamFilepath)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse local file duration: %v", err)
		}
	default:
		return 0, 0, fmt.Errorf("unknown source: %v", song.Source)
	}

	playbackPos := encodingStartTime + streamingPos

	fmt.Printf("Playback stopped at:\t%s,\tTotal Song duration:\t%s\n", playbackPos, songDuration)
	fmt.Printf("Encoding started at:\t%s,\tStreaming delay:\t%s\n", encodingStartTime, streamingDelay)

	return songDuration, playbackPos, nil
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

func extractHostname(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	hostname := parsedURL.Hostname()

	// Remove 'www.' prefix if present
	hostname = strings.TrimPrefix(hostname, "www.")

	return hostname, nil
}
