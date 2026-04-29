// Package player provides a queue-based playback engine with pluggable sinks and resolvers.
package player

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/keshon/melodix/pkg/music/stream"
	"github.com/rs/zerolog"
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

var (
	ErrNoTrackPlaying     = errors.New("no track is currently playing")
	ErrNoTracksInQueue    = errors.New("no tracks in queue")
	ErrNoParsersForTrack  = errors.New("track has no available parsers")
	ErrPauseNotSupported  = errors.New("pause is not supported")
	ErrResumeNotSupported = errors.New("resume is not supported")
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

	log zerolog.Logger

	// stopOnce closes stopPlayback at most once per playback run.
	stopOnce sync.Once
	// stopPlayback signals the active Stream loop to stop (skip, stop, or starting a new track).
	stopPlayback chan struct{}
	// playbackDone is closed when the runPlayback goroutine for the current run exits.
	playbackDone chan struct{}
	// PlayerStatus receives playback lifecycle updates for UI (buffered; drops if full).
	PlayerStatus chan Status

	transportRecoveryMode string
	transportSoftAttempts int
}

type Options struct {
	// Logger is optional. If zero, the player logs nothing.
	Logger zerolog.Logger
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

	l := opts.Logger
	if l.GetLevel() == zerolog.NoLevel {
		l = zerolog.Nop()
	}

	return &Player{
		resolver:              res,
		sinkProvider:          sinkProvider,
		queue:                 make([]parsers.TrackParse, 0),
		stopPlayback:          make(chan struct{}),
		playbackDone:          make(chan struct{}),
		PlayerStatus:          make(chan Status, 10),
		transportRecoveryMode: mode,
		transportSoftAttempts: softAttempts,
		log:                   l,
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
	p.log.Info().Str("input", input).Str("source", source).Str("parser", parser).Msg("enqueue_called")
	tracksInfo, err := p.resolver.Resolve(input, source, parser)
	if err != nil {
		p.log.Warn().Err(err).Msg("resolve_tracks_failed")
		p.emitStatus(StatusError)
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	tracksParse := make([]parsers.TrackParse, 0, len(tracksInfo))
	for _, trackInfo := range tracksInfo {
		if len(trackInfo.AvailableParsers) == 0 {
			p.log.Warn().Str("title", trackInfo.Title).Msg("track_skipped_no_parsers")
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
	p.log.Info().Int("added", len(tracksParse)).Int("queue_len", len(p.queue)).Msg("queue_tracks_added")
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
	p.log.Info().Int("added", 1).Int("queue_len", len(p.queue)).Msg("queue_tracks_added")
	if p.currTrack != nil {
		p.emitStatus(StatusAdded)
	}
	return nil
}

// PlayNext stops current track (if any) and plays the next in queue.
// target is the voice channel ID for Discord, or "" for CLI.
func (p *Player) PlayNext(target string) error {
	p.log.Info().Int("queue_len", len(p.queue)).Msg("play_next_called")
	for {
		if p.IsPlaying() {
			p.log.Info().Msg("stopping_current_before_next")
			_ = p.Stop(false)
		}

		p.playNextMu.Lock()
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.mu.Unlock()
			p.playNextMu.Unlock()
			p.log.Info().Msg("queue_empty")
			return ErrNoTracksInQueue
		}

		track := p.queue[0]
		p.queue = p.queue[1:]
		p.target = target
		p.mu.Unlock()

		p.log.Info().Str("title", track.Title).Str("url", track.URL).Msg("track_attempt_play")

		err := p.startTrack(&track, false)
		p.playNextMu.Unlock()

		if err != nil {
			p.log.Warn().Str("title", track.Title).Err(err).Msg("track_skipped_error")
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

		p.log.Info().Str("title", track.Title).Int("queue_len", len(p.Queue())).Msg("track_now_playing")
		return nil
	}
}

// Stop safely stops current playback. When disconnect is true, clears queue and releases the sink (e.g. leave VC).
func (p *Player) Stop(disconnect bool) error {
	p.log.Info().Bool("disconnect", disconnect).Msg("stop_called")

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
			p.log.Info().Msg("playback_goroutine_done")
		case <-time.After(10 * time.Second):
			p.log.Warn().Msg("stop_timeout_waiting_playback")
		}
	}

	p.mu.Lock()
	p.playing = false
	p.starting = false
	p.currTrack = nil

	if disconnect {
		p.log.Info().Msg("disconnect_and_clear_queue")
		p.queue = nil
		p.target = ""
		p.sinkProvider.ReleaseSink(target)
	}

	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.emitStatus(StatusStopped)
	p.mu.Unlock()

	p.log.Info().Msg("stop_finished")
	return nil
}

// Pause is currently not supported (the sink owns the read loop).
func (p *Player) Pause() error {
	return ErrPauseNotSupported
}

// Resume is currently not supported.
func (p *Player) Resume() error {
	return ErrResumeNotSupported
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
	p.log.Info().
		Str("title", track.Title).
		Str("url", track.URL).
		Str("parser", track.CurrentParser).
		Int("queue_len", len(p.queue)).
		Msg("playback_preparing")

	p.mu.Lock()
	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.starting = true
	p.playing = false
	p.currTrack = track
	p.mu.Unlock()

	rs := stream.NewRecoveryStreamWithLogger(track, p.log)
	if err := rs.Open(0); err != nil {
		p.log.Error().Err(err).Msg("stream_open_failed")
		p.mu.Lock()
		p.starting = false
		p.currTrack = nil
		p.mu.Unlock()
		return err
	}

	if resumed {
		p.emitStatus(StatusResumed)
		p.log.Info().Str("title", track.Title).Msg("track_resuming")
	} else {
		p.emitStatus(StatusPlaying)
		p.log.Info().Str("title", track.Title).Msg("track_starting")
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
			p.log.Warn().Str("title", track.Title).Err(err).Msg("playback_error")
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
		nextErr := p.PlayNext(target)
		if errors.Is(nextErr, ErrNoTracksInQueue) {
			_ = p.Stop(true)
			return
		}
		if nextErr != nil {
			p.log.Warn().Err(nextErr).Msg("play_next_after_track_failed")
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
	p.log.Info().Str("title", title).Msg("playback_running")

	var err error
	softUsed := 0
	for attempt := 1; attempt <= maxVoiceTransportAttempts; attempt++ {
		var audioSink sink.AudioSink
		audioSink, err = p.sinkProvider.Sink(target)
		if err != nil {
			p.log.Warn().Int("attempt", attempt).Int("max", maxVoiceTransportAttempts).Err(err).Msg("sink_get_failed")
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
			p.log.Info().Msg("playback_stopped_by_user")
			p.emitStatus(StatusStopped)
			return err
		}
		if errors.Is(err, stream.ErrVoiceTransport) {
			p.log.Warn().Int("attempt", attempt).Int("max", maxVoiceTransportAttempts).Err(err).Msg("voice_transport_error")

			softTry := recoveryMode == "soft" && softUsed < softAttempts
			if softTry {
				softUsed++
				p.log.Info().Int("used", softUsed).Int("max", softAttempts).Msg("transport_recovery_soft_reopen_stream")
			} else {
				p.log.Info().Msg("transport_recovery_hard_invalidate_sink")
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
		p.log.Warn().Err(err).Msg("playback_finished_error")
		return fmt.Errorf("playback error: %w", err)
	}
	p.log.Info().Msg("playback_stopped")
	p.emitStatus(StatusStopped)

	if len(p.Queue()) == 0 {
		p.log.Info().Msg("queue_empty_auto_stop")
		_ = p.Stop(true)
	}

	return nil
}

func (p *Player) emitStatus(status Status) {
	select {
	case p.PlayerStatus <- status:
	default:
		p.log.Debug().Str("status", string(status)).Msg("player_status_dropped")
	}
}

// ChannelID returns the current target (voice channel ID for Discord, "" for CLI).
func (p *Player) ChannelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.target
}
