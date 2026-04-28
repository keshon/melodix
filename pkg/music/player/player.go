// Package player provides a queue-based playback engine with pluggable sinks and resolvers.
package player

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
)

type Status string

const (
	StatusPlaying Status = "Playing"
	StatusAdded   Status = "Track(s) Added"
	StatusStopped Status = "Playback Stopped"
	StatusPaused  Status = "Playback Paused"
	StatusResumed Status = "Playback Resumed"
	StatusError   Status = "Error"
)

func (status Status) StringEmoji() string {
	m := map[Status]string{
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
	// ErrSinkUnavailable indicates voice/sink could not be obtained (e.g. join timeout or no permission).
	// When runPlayback returns this, the player skips calling PlayNext to avoid spinning through the queue.
	ErrSinkUnavailable = errors.New("sink unavailable")
)

// Resolver resolves input (URL or search query) to track info. The player uses this to enqueue tracks.
// Implementations can be the default resolver (pkg/music/resolve) or a mock/custom resolver.
type Resolver interface {
	Resolve(input, source, parser string) ([]sources.TrackInfo, error)
}

// PlaybackRecorder is called after a track successfully starts (after Open), e.g. to persist guild playback history.
// Discord wiring sets guildID; CLI/examples leave recorder nil.
type PlaybackRecorder interface {
	Record(guildID string, playedAt time.Time, track parsers.TrackParse)
}

type Player struct {
	// mu protects queue, currTrack, playing, starting, target, and the stop/playback fields below.
	mu sync.Mutex
	// playing is true while PCM is streaming after the stream has opened successfully.
	playing bool
	// starting is true while the current track is still resolving/opening; IsPlaying is playing || starting.
	starting bool
	// playNextMu ensures only one goroutine at a time runs dequeue + startTrack (including the slow Open phase),
	// so concurrent PlayNext or Resume cannot start two tracks.
	playNextMu sync.Mutex
	// currTrack is the track being opened or actively playing (nil when idle).
	currTrack *parsers.TrackParse
	// queue holds tracks waiting to play (FIFO).
	queue []parsers.TrackParse

	// resolver turns user input into track metadata for enqueue.
	resolver Resolver
	// sinkProvider supplies the audio sink (e.g. Discord VC or speaker) for a target channel.
	sinkProvider sink.Provider

	// target is the voice channel ID for Discord playback, or "" for CLI/non-voice.
	target string
	// guildID is set by the Discord voice layer for playback recording; empty for CLI.
	guildID string
	// recorder persists successful starts (nil for CLI).
	recorder PlaybackRecorder

	// stopOnce closes stopPlayback at most once per playback run.
	stopOnce sync.Once
	// stopPlayback signals the active Stream loop to stop (skip, stop, or starting a new track).
	stopPlayback chan struct{}
	// playbackDone is closed when the runPlayback goroutine for the current run exits.
	playbackDone chan struct{}
	// PlayerStatus receives playback lifecycle updates for UI (buffered; drops if full).
	PlayerStatus chan Status

	transportRecoveryMode  string
	transportSoftAttempts  int
}

type Options struct {
	// TransportRecoveryMode controls behavior on stream.ErrVoiceTransport.
	// Supported: "hard" (default), "soft".
	TransportRecoveryMode string
	// TransportSoftAttempts bounds how many soft retries we do before falling back to hard recovery.
	// Applies to mode="soft" only. Default 1.
	TransportSoftAttempts int
}

// New creates a new Player. target is set per playback via PlayNext(target).
func New(sinkProvider sink.Provider, res Resolver) *Player {
	return NewWithOptions(sinkProvider, res, Options{})
}

// NewWithOptions creates a new Player with custom options.
func NewWithOptions(sinkProvider sink.Provider, res Resolver, opts Options) *Player {
	mode := opts.TransportRecoveryMode
	if mode == "" {
		mode = "hard"
	}
	softAttempts := opts.TransportSoftAttempts
	if softAttempts <= 0 {
		softAttempts = 1
	}

	return &Player{
		resolver:     res,
		sinkProvider: sinkProvider,
		queue:        make([]parsers.TrackParse, 0),
		stopPlayback: make(chan struct{}),
		playbackDone: make(chan struct{}),
		PlayerStatus: make(chan Status, 10),
		transportRecoveryMode: mode,
		transportSoftAttempts: softAttempts,
	}
}

// SetGuildID sets the Discord guild id for this player (used when invoking the playback recorder).
func (p *Player) SetGuildID(guildID string) {
	p.mu.Lock()
	p.guildID = guildID
	p.mu.Unlock()
}

// SetRecorder sets an optional callback invoked after a track successfully starts. Pass nil to disable.
func (p *Player) SetRecorder(r PlaybackRecorder) {
	p.mu.Lock()
	p.recorder = r
	p.mu.Unlock()
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
		if p.IsPlaying() {
			log.Printf("[Player] Stopping current track before playing next")
			_ = p.Stop(false)
		}

		p.playNextMu.Lock()
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.mu.Unlock()
			p.playNextMu.Unlock()
			log.Printf("[Player] Queue is empty, nothing to play")
			return ErrNoTracksInQueue
		}

		track := p.queue[0]
		p.queue = p.queue[1:]
		p.target = target
		p.mu.Unlock()

		log.Printf("[Player] Attempting to play track %q (%s)", track.Title, track.URL)

		err := p.startTrack(&track, false)
		p.playNextMu.Unlock()

		if err != nil {
			log.Printf("[Player] Skipping track %q due to error: %v", track.Title, err)
			continue
		}

		playedAt := time.Now()
		p.mu.Lock()
		gid := p.guildID
		rec := p.recorder
		p.mu.Unlock()
		if rec != nil && gid != "" {
			// Future: listened-duration aggregation would require completion callbacks from here or runPlayback.
			rec.Record(gid, playedAt, cloneTrackParse(track))
		}

		log.Printf("[Player] Now playing track %q | QueueLen=%d", track.Title, len(p.Queue()))
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
		select {
		case <-doneCh:
			log.Printf("[Player] Playback goroutine finished")
		case <-time.After(10 * time.Second):
			log.Printf("[Player] Stop timed out waiting for playback goroutine; cleaning up anyway")
		}
	}

	p.mu.Lock()
	p.playing = false
	p.starting = false
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
	p.mu.Unlock()

	p.playNextMu.Lock()
	defer p.playNextMu.Unlock()
	return p.startTrack(track, true)
}

// IsPlaying returns true while a track is opening or actively playing (excludes paused state where playing is false).
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing || p.starting
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

func cloneTrackParse(tp parsers.TrackParse) parsers.TrackParse {
	out := tp
	if len(tp.SourceInfo.AvailableParsers) > 0 {
		out.SourceInfo.AvailableParsers = slices.Clone(tp.SourceInfo.AvailableParsers)
	}
	return out
}

// startTrack launches playback goroutine
func (p *Player) startTrack(track *parsers.TrackParse, resumed bool) error {
	log.Printf("[Player] Preparing playback for track: %q (%s) | CurrentParser=%s | QueueLen=%d",
		track.Title, track.URL, track.CurrentParser, len(p.queue))

	p.mu.Lock()
	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.starting = true
	p.playing = false
	p.currTrack = track
	p.mu.Unlock()

	rs := stream.NewRecoveryStream(track)
	if err := rs.Open(0); err != nil {
		log.Printf("[Player] Failed to open resilient stream: %v", err)
		p.mu.Lock()
		p.starting = false
		p.currTrack = nil
		p.mu.Unlock()
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
	p.starting = false
	p.playing = true
	p.currTrack = track
	stopCh := p.stopPlayback
	doneCh := p.playbackDone
	p.mu.Unlock()

	go func() {
		if err := p.runPlayback(rs, stopCh, doneCh); err != nil {
			log.Printf("[Player] Playback error for track %q: %v", track.Title, err)
			if errors.Is(err, ErrSinkUnavailable) {
				return
			}
			if errors.Is(err, stream.ErrVoiceTransport) {
				return
			}
			if errors.Is(err, stream.ErrPlaybackStopped) {
				return
			}
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

// maxVoiceTransportAttempts bounds Sink rejoin + Opus transport retries for one track
// (Discord gateway/voice), distinct from RecoveryStream media recovery.
const maxVoiceTransportAttempts = 3

// runPlayback streams to the sink. stopCh and doneCh are for this run only.
func (p *Player) runPlayback(rs *stream.RecoveryStream, stopCh, doneCh chan struct{}) error {
	defer rs.Close()
	defer close(doneCh)

	p.mu.Lock()
	ct := p.currTrack
	target := p.target
	recoveryMode := p.transportRecoveryMode
	softAttempts := p.transportSoftAttempts
	p.mu.Unlock()

	title := "(unknown)"
	if ct != nil {
		title = ct.Title
	}
	log.Printf("[Player] Running playback for track: %q", title)

	var err error
	softUsed := 0
	for attempt := 1; attempt <= maxVoiceTransportAttempts; attempt++ {
		var audioSink sink.AudioSink
		audioSink, err = p.sinkProvider.Sink(target)
		if err != nil {
			log.Printf("[Player] Failed to get sink (attempt %d/%d): %v", attempt, maxVoiceTransportAttempts, err)
			p.sinkProvider.InvalidateSink()
			if attempt == maxVoiceTransportAttempts {
				p.mu.Lock()
				p.playing = false
				p.currTrack = nil
				p.mu.Unlock()
				p.emitStatus(StatusError)
				return errors.Join(ErrSinkUnavailable, fmt.Errorf("get sink: %w", err))
			}
			time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
			continue
		}

		err = audioSink.Stream(rs, stopCh)
		if err == nil {
			break
		}
		if errors.Is(err, stream.ErrPlaybackStopped) {
			p.mu.Lock()
			p.playing = false
			p.currTrack = nil
			p.mu.Unlock()
			log.Printf("[Player] Playback stopped by user")
			p.emitStatus(StatusStopped)
			return err
		}
		if errors.Is(err, stream.ErrVoiceTransport) {
			log.Printf("[Player] Voice transport error (attempt %d/%d): %v", attempt, maxVoiceTransportAttempts, err)

			softTry := recoveryMode == "soft" && softUsed < softAttempts
			if softTry {
				softUsed++
				log.Printf("[Player] Transport recovery mode=soft (%d/%d): reopening stream without voice reconnect", softUsed, softAttempts)
			} else {
				log.Printf("[Player] Transport recovery: invalidating voice sink (hard fallback)")
				p.sinkProvider.InvalidateSink()
			}

			if reopenErr := rs.ReopenAfterTransportFailure(); reopenErr != nil {
				p.mu.Lock()
				p.playing = false
				p.currTrack = nil
				p.mu.Unlock()
				p.emitStatus(StatusError)
				return fmt.Errorf("voice transport failed, could not reopen stream: %w", reopenErr)
			}
			if attempt == maxVoiceTransportAttempts {
				p.mu.Lock()
				p.playing = false
				p.currTrack = nil
				p.mu.Unlock()
				p.emitStatus(StatusError)
				return err
			}
			time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
			continue
		}
		break
	}

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

func (p *Player) emitStatus(status Status) {
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
