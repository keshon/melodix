package player

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/keshon/dca"
	songpkg "github.com/keshon/melodix/song"
	"github.com/keshon/melodix/storage"
	"github.com/keshon/melodix/ytdlp"
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

		options := dca.StdEncodeOptions
		options.RawOutput = true
		options.Bitrate = 96
		options.Application = "lowdelay"
		options.FrameDuration = 20
		options.BufferedFrames = 400
		options.CompressionLevel = 10
		options.VBR = true
		options.VolumeFloat = 1.0
		options.StartTime = startAt
		options.EncodingLineLog = true

		streamPath := p.Song.StreamURL
		cacheReady := make(chan bool)
		if !isCached {
			go func() {
				cacheDir := "./cache"
				if err := os.MkdirAll(cacheDir, 0755); err != nil {
					fmt.Printf("Error creating cache directory: %v\n", err)
					return
				}

				path, err := ytdlp.New().GetStream(p.Song.PublicLink)
				if err != nil {
					fmt.Printf("Error caching: %v\n", err)
					return
				}

				p.Song.StreamURL = path
				isCached = true // Mark as cached
				cacheReady <- true
				fmt.Println("DOING SWITCH")
			}()
		}

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
			err := p.Storage.AddTrackCountByOne(p.GuildID, p.Song.SongID, p.Song.Title, p.Song.Source.String(), p.Song.PublicLink)
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
						err := p.Storage.AddTrackDuration(p.GuildID, p.Song.SongID, p.Song.Title, p.Song.Source.String(), p.Song.PublicLink, 2*time.Second)
						if err != nil {
							fmt.Printf("Error saving track duration: %v\n", err)
						}
					}
				}
			}
		}()

		for {
			select {
			case err := <-done:
				close(done)
				// stop if there is an error
				if err != nil && err != io.EOF {
					return err
				}
				// restart if there is an interrupt
				duration, position, err := p.getPlaybackDuration(encoding, streaming, p.Song)
				if err != nil {
					return err
				}
				if encoding.Stats().Duration.Seconds() > 0 && position.Seconds() > 0 && position < duration {
					fmt.Printf("Playback interrupted, restarting: \"%v\" from %vs\n", p.Song.Title, position.Seconds())
					encoding.Stop()
					encoding.Cleanup()
					startAt = position
					continue PLAYBACK_LOOP
				}
				// skip to the next song
				if len(p.Queue) > 0 {
					startAt = 0
					encoding.Stop()
					encoding.Cleanup()
					p.Song = nil
					continue PLAYBACK_LOOP
				}
				// finished
				fmt.Printf("Finished playback of \"%v\"", p.Song.Title)
				return nil
			case <-cacheReady: // Cache is ready
				fmt.Println("Switching to cached playback")
				encoding.Stop()
				encoding.Cleanup()
				//streamPath = filename
				continue PLAYBACK_LOOP

			case signal := <-p.ActionSignals:
				switch signal {
				case ActionSkip:
					if len(p.Queue) > 0 {
						startAt = 0
						encoding.Stop()
						encoding.Cleanup()
						p.Song = nil
						continue PLAYBACK_LOOP
					}
					p.ActionSignals <- ActionStop
				case ActionStop:
					return p.leaveVoiceChannel(vc)
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
func (s *Player) streamToFile(stream io.Reader) (string, error) {
	cacheDir := "./cache"
	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	tempFile, err := os.CreateTemp(cacheDir, "*.mp4")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, stream)
	if err != nil {
		return "", fmt.Errorf("failed to write stream to temp file: %v", err)
	}

	return tempFile.Name(), nil
}

func (p *Player) streamToFileWithRetry(streamProvider func() (io.Reader, error), retries int, delay time.Duration) (string, error) {
	cacheDir := "./cache"
	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	var tempFilePath string
	var err error

	for attempt := 1; attempt <= retries; attempt++ {
		fmt.Printf("Attempt %d/%d to write stream to file...\n", attempt, retries)

		// Create a new stream for each attempt
		stream, err := streamProvider()
		if err != nil {
			fmt.Printf("Error retrieving stream on attempt %d: %v\n", attempt, err)
			time.Sleep(delay)
			continue
		}

		// Create a temporary file
		tempFile, err := os.CreateTemp(cacheDir, "*.mp4")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file: %v", err)
		}
		tempFilePath = tempFile.Name()
		defer tempFile.Close()

		// Write the stream to the file
		written, err := io.Copy(tempFile, stream)
		if err == nil && written > 0 {
			fmt.Printf("Successfully wrote %d bytes to %s\n", written, tempFilePath)
			return tempFilePath, nil
		}

		// Clean up and retry if writing failed
		fmt.Printf("Error writing stream on attempt %d: %v\n", attempt, err)
		os.Remove(tempFilePath) // Clean up the failed file
		time.Sleep(delay)
		delay *= 2 // Exponential backoff
	}

	return "", fmt.Errorf("failed to write stream to file after %d attempts: %v", retries, err)
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

	playbackPos := encodingStartTime + streamingPos + streamingDelay.Abs() // the delay is wrong here, but I'm out of ideas how to fix the precision

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
