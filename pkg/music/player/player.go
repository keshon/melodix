package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/source_resolver"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"

	"github.com/bwmarrin/discordgo"
)

type Output int

const (
	OutputDiscord Output = iota
	OutputSpeaker
)

type PlayerStatus string

const (
	StatusPlaying PlayerStatus = "Playing"
	StatusAdded   PlayerStatus = "Track(s) Added"
	StatusStopped PlayerStatus = "Playback Stopped"
	StatusPaused  PlayerStatus = "Playback Paused"
	StatusResumed PlayerStatus = "Playback Resumed"
	StatusError   PlayerStatus = "Error"
)

func (status PlayerStatus) StringEmoji() string {
	m := map[PlayerStatus]string{
		StatusPlaying: "▶️",
		StatusAdded:   "🎶",
		StatusStopped: "⏹",
		StatusPaused:  "⏸",
		StatusResumed: "▶️",
		StatusError:   "❌",
	}
	return m[status]
}

var (
	ErrNoTrackPlaying    = errors.New("no track is currently playing")
	ErrNoTracksInQueue   = errors.New("no tracks in queue")
	ErrNoParsersForTrack = errors.New("track has no available parsers")
)

type Player struct {
	mu        sync.Mutex
	playing   bool
	currTrack *parsers.TrackParse
	queue     []parsers.TrackParse
	history   []parsers.TrackParse
	output    Output

	resolver *source_resolver.SourceResolver
	store    *storage.Storage
	dg       *discordgo.Session

	guildID         string
	channelID       string
	vc              *discordgo.VoiceConnection
	voiceReadyDelay time.Duration // delay after join before sending opus (discordgo op 4 race)

	// playback lifecycle channels and sync
	stopOnce     sync.Once
	stopPlayback chan struct{}
	playbackDone chan struct{}
	PlayerStatus chan PlayerStatus
}

// New creates a new Player instance. voiceReadyDelay is the wait after joining VC before sending opus (0 = default 500ms).
func New(dg *discordgo.Session, guildID string, resolver *source_resolver.SourceResolver, voiceReadyDelay time.Duration) *Player {
	if voiceReadyDelay <= 0 {
		voiceReadyDelay = 500 * time.Millisecond
	}
	return &Player{
		dg:      dg,
		guildID: guildID,

		resolver:        resolver,
		voiceReadyDelay: voiceReadyDelay,
		queue:           make([]parsers.TrackParse, 0),
		history:         make([]parsers.TrackParse, 0),
		stopPlayback:    make(chan struct{}),
		playbackDone:    make(chan struct{}),
		PlayerStatus:    make(chan PlayerStatus, 10), // buffered to reduce drops
	}
}

// Enqueue adds tracks to the queue
func (p *Player) Enqueue(input string, source string, parser string) error {
	log.Printf("[Player] Enqueue called | input=%q source=%q parser=%q", input, source, parser)
	tracksInfo, err := p.resolver.Resolve(input, source, parser)
	if err != nil {
		log.Printf("[Player] Failed to resolve tracks: %v", err)
		p.emitStatus(StatusError)
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	tracksParse := make([]parsers.TrackParse, 0, len(tracksInfo))
	for _, trackInfo := range tracksInfo {
		if len(trackInfo.AvailableParsers) == 0 {
			log.Printf("[Player] Skipping track with no available parsers: %q", trackInfo.Title)
			continue
		}
		tracksParse = append(tracksParse, parsers.TrackParse{
			URL:           trackInfo.URL,
			Title:         trackInfo.Title,
			CurrentParser: trackInfo.AvailableParsers[0],
			SourceInfo:    trackInfo,
		})
	}
	if len(tracksParse) == 0 {
		p.emitStatus(StatusError)
		return ErrNoParsersForTrack
	}

	p.queue = append(p.queue, tracksParse...)
	log.Printf("[Player] Added %d track(s) to queue | QueueLen=%d", len(tracksParse), len(p.queue))
	if p.currTrack != nil {
		p.emitStatus(StatusAdded)
	}
	return nil
}

// EnqueueTrackInfo enqueues a single pre-resolved track (avoids double resolve when caller already has TrackInfo).
func (p *Player) EnqueueTrackInfo(trackInfo sources.TrackInfo) error {
	if len(trackInfo.AvailableParsers) == 0 {
		p.emitStatus(StatusError)
		return ErrNoParsersForTrack
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = append(p.queue, parsers.TrackParse{
		URL:           trackInfo.URL,
		Title:         trackInfo.Title,
		CurrentParser: trackInfo.AvailableParsers[0],
		SourceInfo:    trackInfo,
	})
	log.Printf("[Player] Added 1 track(s) to queue | QueueLen=%d", len(p.queue))
	if p.currTrack != nil {
		p.emitStatus(StatusAdded)
	}
	return nil
}

// PlayNext stops current track (if any) and plays the next in queue
func (p *Player) PlayNext(channelID string) error {
	log.Printf("[Player] PlayNext called | QueueLen=%d", len(p.queue))
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.mu.Unlock()
			log.Printf("[Player] Queue is empty, nothing to play")
			return ErrNoTracksInQueue
		}

		track := p.queue[0]
		p.queue = p.queue[1:]
		p.channelID = channelID
		p.mu.Unlock()

		log.Printf("[Player] Attempting to play track %q (%s)", track.Title, track.URL)

		if p.IsPlaying() {
			log.Printf("[Player] Stopping current track before playing next")
			_ = p.Stop(false)
		}

		err := p.startTrack(&track, false)
		if err != nil {
			log.Printf("[Player] Skipping track %q due to error: %v", track.Title, err)
			continue // try next track
		}

		p.mu.Lock()
		p.currTrack = &track
		p.playing = true
		p.history = append(p.history, track)
		p.mu.Unlock()

		log.Printf("[Player] Now playing track %q | QueueLen=%d", track.Title, len(p.queue))
		return nil
	}
}

// Stop safely stops current playback
func (p *Player) Stop(exitVc bool) error {

	log.Printf("[Player] Stop called | exitVc=%v", exitVc)

	// Capture doneCh and close stop under lock so startTrack cannot replace them in between.
	// We must wait on this exact doneCh so we unblock when the run we're stopping exits.
	var doneCh chan struct{}
	p.mu.Lock()
	doneCh = p.playbackDone
	p.stopOnce.Do(func() {
		close(p.stopPlayback)
	})
	p.mu.Unlock()

	// wait only if something is playing (doneCh is the channel that run will close)
	if p.IsPlaying() && doneCh != nil {
		<-doneCh
		log.Printf("[Player] Playback goroutine finished")
	}

	p.mu.Lock()
	p.playing = false
	p.currTrack = nil

	if exitVc {
		log.Printf("[Player] Exiting voice channel and clearing queue")
		p.queue = nil
		p.channelID = ""

		if p.vc != nil {
			err := p.vc.Disconnect(context.Background())
			if err != nil {
				log.Printf("[Player] Error during VC disconnect: %v", err)
			}
			p.vc = nil
		}
	}

	// reinitialize channels and stopOnce for next playback session
	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.emitStatus(StatusStopped)
	p.mu.Unlock()

	log.Printf("[Player] Stop finished")
	return nil
}

// Pause pauses playback. Note: Not wired to actual streaming — only sets playing=false and emits StatusPaused.
// StreamToDiscord continues sending; audio does not stop. Resume() would start a second stream. Not exposed in slash commands.
// If you add a pause command later, reimplement by closing stopPlayback and waiting for playbackDone; Resume by opening a new stream from current position.
func (p *Player) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.playing {
		return ErrNoTrackPlaying
	}
	p.playing = false
	p.emitStatus(StatusPaused)
	return nil
}

// Resume resumes playback. Starts a new playback goroutine; do not use while the previous runPlayback is still active (see Pause comment).
func (p *Player) Resume() error {
	p.mu.Lock()
	if p.currTrack == nil {
		p.mu.Unlock()
		return ErrNoTrackPlaying
	}
	track := p.currTrack
	p.playing = true
	p.mu.Unlock()

	// Restart playback for resume
	return p.startTrack(track, true)
}

// IsPlaying returns current playback state
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// CurrentTrack returns currently playing track (nil if none)
func (p *Player) CurrentTrack() *parsers.TrackParse {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currTrack
}

// Queue returns a copy of current queue
func (p *Player) Queue() []parsers.TrackParse {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.queue)
}

// History returns a copy of played tracks
func (p *Player) History() []parsers.TrackParse {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.history)
}

// startTrack launches playback goroutine
func (p *Player) startTrack(track *parsers.TrackParse, resumed bool) error {
	log.Printf("[Player] Preparing playback for track: %q (%s) | CurrentParser=%s | QueueLen=%d",
		track.Title, track.URL, track.CurrentParser, len(p.queue))

	// Reinitialize lifecycle channels for this playback run.
	// We do this under lock so there is no race between Stop() and a new start.
	p.mu.Lock()
	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.mu.Unlock()

	rs := stream.NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		log.Printf("[Player] Failed to open resilient stream: %v", err)
		return err
	}

	if resumed {
		p.emitStatus(StatusResumed)
		log.Printf("[Player] Resuming track %q", track.Title)
	} else {
		p.emitStatus(StatusPlaying)
		log.Printf("[Player] Starting track %q", track.Title)
	}

	p.mu.Lock()
	p.currTrack = track
	p.playing = true
	stopCh := p.stopPlayback
	doneCh := p.playbackDone
	p.mu.Unlock()

	go func() {
		// runPlayback closes doneCh on exit so Stop() unblocks on the correct channel.
		if err := p.runPlayback(rs, stopCh, doneCh); err != nil {
			log.Printf("[Player] Playback error for track %q: %v", track.Title, err)
		}

		// Attempt to play next track automatically (if any). Only if we still have a channel
		// (Stop(true) clears channelID; avoid calling PlayNext with empty channel).
		p.mu.Lock()
		chID := p.channelID
		p.mu.Unlock()
		if chID != "" {
			if nextErr := p.PlayNext(chID); nextErr != nil && !errors.Is(nextErr, ErrNoTracksInQueue) {
				log.Printf("[Player] PlayNext after track failed: %v", nextErr)
			}
		}
	}()

	return nil
}

// runPlayback handles actual streaming. stopCh and doneCh are the channels for this run;
// closing doneCh on exit unblocks Stop() even if the player's channels were replaced by a new startTrack.
func (p *Player) runPlayback(rs io.ReadCloser, stopCh, doneCh chan struct{}) error {
	// Ensure we close stream and signal done exactly once.
	defer rs.Close()
	defer close(doneCh)

	var err error
	// Guard access to currTrack safely for logging
	p.mu.Lock()
	ct := p.currTrack
	p.mu.Unlock()

	title := "(unknown)"
	if ct != nil {
		title = ct.Title
	}

	log.Printf("[Player] Running playback for track: %q", title)

	p.mu.Lock()
	output := p.output
	voiceReadyDelay := p.voiceReadyDelay
	p.mu.Unlock()

	switch output {
	case OutputSpeaker:
		log.Printf("[Player] Output mode: Speaker (not implemented)")
	default:
		vc, vErr := p.getOrCreateVoiceConnection()
		if vErr != nil {
			err = vErr
			log.Printf("[Player] Failed to get/create voice connection: %v", vErr)
		} else {
			p.mu.Lock()
			p.vc = vc
			p.mu.Unlock()
			// Wait for voice encryption (op 4) to be applied before sending opus.
			// discordgo starts opusSender on op 2 but aead is set on op 4; sending
			// before op 4 causes nil pointer panic in opusSender.
			time.Sleep(voiceReadyDelay)
			log.Printf("[Player] Streaming to Discord VC: status=%v guild=%s", vc.Status, p.guildID)
			if streamErr := stream.StreamToDiscord(rs, stopCh, vc); streamErr != nil {
				err = streamErr
				log.Printf("[Player] StreamToDiscord error: %v", streamErr)
			}
		}
	}

	p.mu.Lock()
	p.playing = false
	p.currTrack = nil
	p.mu.Unlock()

	if err != nil {
		err = fmt.Errorf("playback error: %w", err)
		p.emitStatus(StatusError)
		log.Printf("[Player] Playback finished with error: %v", err)
	} else {
		log.Printf("[Player] Playback stopped")
		p.emitStatus(StatusStopped)
	}

	// Auto-stop (disconnect) when queue empty
	if len(p.Queue()) == 0 {
		log.Printf("[Player] Queue empty after track, auto-stopping player")
		// call Stop(true) but ignore its error here
		_ = p.Stop(true)
	}

	return err
}

// getOrCreateVoiceConnection joins or reuses existing VC. Caller must not hold p.mu.
func (p *Player) getOrCreateVoiceConnection() (*discordgo.VoiceConnection, error) {
	p.mu.Lock()
	channelID := p.channelID
	vc := p.vc
	p.mu.Unlock()

	if channelID == "" {
		return nil, errors.New("voice channel ID is not set")
	}
	if vc != nil {
		return vc, nil
	}

	vc, err := p.dg.ChannelVoiceJoin(context.Background(), p.guildID, channelID, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	p.mu.Lock()
	p.vc = vc
	p.mu.Unlock()
	log.Printf("[Player] Joined voice channel %s on guild %s", channelID, p.guildID)
	return vc, nil
}

// emitStatus safely sends player status
func (p *Player) emitStatus(status PlayerStatus) {
	select {
	case p.PlayerStatus <- status:
	default:
		log.Printf("[Player] Player status signal dropped (channel full) - %s", status)
	}
}

// SetOutput sets playback output
func (p *Player) SetOutput(mode Output) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.output = mode
}

func (p *Player) ChannelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.channelID
}
