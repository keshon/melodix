package player

import (
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"sync"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
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

// Resolver resolves input (URL or search query) to track info. The player uses this to enqueue tracks.
// Implementations can be the default resolver (pkg/music/resolver) or a mock/custom resolver.
type Resolver interface {
	Resolve(input, source, parser string) ([]sources.TrackInfo, error)
}

type Player struct {
	mu        sync.Mutex
	playing   bool
	currTrack *parsers.TrackParse
	queue     []parsers.TrackParse
	history   []parsers.TrackParse

	resolver     Resolver
	sinkProvider sink.SinkProvider

	target string // voice channel ID for Discord, "" for CLI

	// playback lifecycle channels and sync
	stopOnce     sync.Once
	stopPlayback chan struct{}
	playbackDone chan struct{}
	PlayerStatus chan PlayerStatus
}

// New creates a new Player. target is set per playback via PlayNext(target).
func New(sinkProvider sink.SinkProvider, res Resolver) *Player {
	return &Player{
		resolver:     res,
		sinkProvider: sinkProvider,
		queue:        make([]parsers.TrackParse, 0),
		history:      make([]parsers.TrackParse, 0),
		stopPlayback: make(chan struct{}),
		playbackDone: make(chan struct{}),
		PlayerStatus: make(chan PlayerStatus, 10),
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

// PlayNext stops current track (if any) and plays the next in queue.
// target is the voice channel ID for Discord, or "" for CLI.
func (p *Player) PlayNext(target string) error {
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
		p.target = target
		p.mu.Unlock()

		log.Printf("[Player] Attempting to play track %q (%s)", track.Title, track.URL)

		if p.IsPlaying() {
			log.Printf("[Player] Stopping current track before playing next")
			_ = p.Stop(false)
		}

		err := p.startTrack(&track, false)
		if err != nil {
			log.Printf("[Player] Skipping track %q due to error: %v", track.Title, err)
			continue
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

// Stop safely stops current playback. When disconnect is true, clears queue and releases the sink (e.g. leave VC).
func (p *Player) Stop(disconnect bool) error {
	log.Printf("[Player] Stop called | disconnect=%v", disconnect)

	var doneCh chan struct{}
	p.mu.Lock()
	doneCh = p.playbackDone
	p.stopOnce.Do(func() {
		close(p.stopPlayback)
	})
	target := p.target
	p.mu.Unlock()

	if p.IsPlaying() && doneCh != nil {
		<-doneCh
		log.Printf("[Player] Playback goroutine finished")
	}

	p.mu.Lock()
	p.playing = false
	p.currTrack = nil

	if disconnect {
		log.Printf("[Player] Disconnecting and clearing queue")
		p.queue = nil
		p.target = ""
		p.sinkProvider.ReleaseSink(target)
	}

	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.emitStatus(StatusStopped)
	p.mu.Unlock()

	log.Printf("[Player] Stop finished")
	return nil
}

// Pause pauses playback. Note: Not wired to actual streaming — only sets playing=false and emits StatusPaused.
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

// Resume resumes playback.
func (p *Player) Resume() error {
	p.mu.Lock()
	if p.currTrack == nil {
		p.mu.Unlock()
		return ErrNoTrackPlaying
	}
	track := p.currTrack
	p.playing = true
	p.mu.Unlock()
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
		if err := p.runPlayback(rs, stopCh, doneCh); err != nil {
			log.Printf("[Player] Playback error for track %q: %v", track.Title, err)
		}

		p.mu.Lock()
		target := p.target
		p.mu.Unlock()
		if nextErr := p.PlayNext(target); nextErr != nil && !errors.Is(nextErr, ErrNoTracksInQueue) {
			log.Printf("[Player] PlayNext after track failed: %v", nextErr)
		}
	}()

	return nil
}

// runPlayback streams to the sink. stopCh and doneCh are for this run only.
func (p *Player) runPlayback(rs io.ReadCloser, stopCh, doneCh chan struct{}) error {
	defer rs.Close()
	defer close(doneCh)

	p.mu.Lock()
	ct := p.currTrack
	target := p.target
	p.mu.Unlock()

	title := "(unknown)"
	if ct != nil {
		title = ct.Title
	}
	log.Printf("[Player] Running playback for track: %q", title)

	audioSink, err := p.sinkProvider.GetSink(target)
	if err != nil {
		log.Printf("[Player] Failed to get sink: %v", err)
		p.mu.Lock()
		p.playing = false
		p.currTrack = nil
		p.mu.Unlock()
		p.emitStatus(StatusError)
		return fmt.Errorf("get sink: %w", err)
	}

	err = audioSink.Stream(rs, stopCh)

	p.mu.Lock()
	p.playing = false
	p.currTrack = nil
	p.mu.Unlock()

	if err != nil {
		p.emitStatus(StatusError)
		log.Printf("[Player] Playback finished with error: %v", err)
		return fmt.Errorf("playback error: %w", err)
	}
	log.Printf("[Player] Playback stopped")
	p.emitStatus(StatusStopped)

	if len(p.Queue()) == 0 {
		log.Printf("[Player] Queue empty after track, auto-stopping player")
		_ = p.Stop(true)
	}

	return nil
}

func (p *Player) emitStatus(status PlayerStatus) {
	select {
	case p.PlayerStatus <- status:
	default:
		log.Printf("[Player] Player status signal dropped (channel full) - %s", status)
	}
}

// ChannelID returns the current target (voice channel ID for Discord, "" for CLI).
func (p *Player) ChannelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.target
}
