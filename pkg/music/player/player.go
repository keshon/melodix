// Package player provides a queue-based playback engine with pluggable sinks
// and resolvers. It is the Discord-free half of playback: one Player drives one
// target (a guild's voice channel, or the CLI's speaker).
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

// Status is a playback lifecycle event delivered on Player.PlayerStatus.
type Status string

// TransportRecoveryMode selects how the player reacts to voice transport
// failures (stream.ErrVoiceTransport from the sink).
type TransportRecoveryMode string

const (
	// RecoveryHard invalidates the sink (forcing a voice rejoin) before
	// reopening the media stream. The safe default.
	RecoveryHard TransportRecoveryMode = "hard"
	// RecoverySoft reopens the media stream first and falls back to hard
	// recovery after Options.TransportSoftAttempts.
	RecoverySoft TransportRecoveryMode = "soft"
)

// ParseTransportRecoveryMode maps a config string to a mode; ok is false for
// unknown values (callers should warn and use RecoveryHard).
func ParseTransportRecoveryMode(s string) (TransportRecoveryMode, bool) {
	switch TransportRecoveryMode(s) {
	case RecoveryHard, RecoverySoft:
		return TransportRecoveryMode(s), true
	default:
		return RecoveryHard, false
	}
}

const (
	StatusPlaying Status = "Playing"
	StatusAdded   Status = "Track(s) Added"
	StatusStopped Status = "Playback Stopped"
	StatusPaused  Status = "Playback Paused"
	StatusResumed Status = "Playback Resumed"
	StatusError   Status = "Error"
)

var (
	ErrNoTrackPlaying  = errors.New("no track is currently playing")
	ErrNoTracksInQueue = errors.New("no tracks in queue")
	// ErrTrackStartFailed means a dequeued track could not start (e.g. every
	// parser failed) and the queue held nothing further to fall forward to.
	ErrTrackStartFailed   = errors.New("track failed to start")
	ErrNoParsersForTrack  = errors.New("track has no available parsers")
	ErrPauseNotSupported  = errors.New("pause is not supported")
	ErrResumeNotSupported = errors.New("resume is not supported")
	// ErrSinkUnavailable means no sink could be obtained — a join timeout, or
	// missing Connect/Speak permission. runPlayback returning this suppresses the
	// usual advance to PlayNext: nothing is wrong with the track, so walking the
	// queue would burn every entry against the same unavailable channel.
	ErrSinkUnavailable = errors.New("sink unavailable")
)

// Resolver turns input (a URL or a search query) into track metadata for
// Enqueue. It is an interface so the player stays testable against a fake;
// production wires pkg/music/resolve.
type Resolver interface {
	Resolve(input, source, parser string) ([]sources.TrackInfo, error)
}

// PlaybackRecorder is called once a track has actually produced audio — the
// first packet, not a successful Open — to persist guild playback history.
// Track carries the parser that produced it, so a parser that opened and then
// died is never recorded. Discord wiring sets guildID; CLI leaves recorder nil.
type PlaybackRecorder interface {
	Record(guildID string, playedAt time.Time, track parsers.Track)
}

// Player is a queue-based playback engine: it resolves input through a
// Resolver, opens tracks via the parser registry with recovery, and streams the
// resulting Opus packets to an AudioSink. One Player per playback target.
type Player struct {
	// mu protects queue, currTrack, playing, starting, target, and the
	// stop/playback fields below.
	mu sync.Mutex
	// playing is true once the stream is open and Opus packets are flowing to
	// the sink.
	playing bool
	// starting is true while the current track is still resolving or opening;
	// IsPlaying is playing || starting.
	starting bool
	// playNextMu serializes dequeue + startTrack, the slow Open phase included,
	// so two concurrent PlayNext calls cannot start two tracks.
	playNextMu sync.Mutex
	// currTrack is the track being opened or actively playing (nil when idle).
	currTrack *parsers.Track
	// queue holds tracks waiting to play (FIFO).
	queue []parsers.Track

	// resolver turns user input into track metadata for enqueue.
	resolver Resolver
	// sinkProvider supplies the audio sink (Discord voice, or speaker) for a
	// target channel.
	sinkProvider sink.Provider

	// target is the voice channel id for Discord, or "" for CLI/non-voice.
	target string
	// guildID is set by the Discord voice layer for playback recording; it is
	// empty for the CLI.
	guildID string
	// recorder persists successful starts (nil for CLI).
	recorder PlaybackRecorder
	// announcedParser is the parser the last emitted status told the UI about,
	// updated to whatever actually plays as each confirmation arrives. The two
	// diverge when a parser opens, is announced, then dies on its first read —
	// the case this whole handshake exists to correct.
	announcedParser string
	// recorded is true once a history row exists for the current track.
	recorded bool

	log zerolog.Logger

	// stopOnce closes stopPlayback at most once per playback run.
	stopOnce sync.Once
	// stopPlayback signals the active Stream loop to stop — a skip, a stop, or a
	// new track starting.
	stopPlayback chan struct{}
	// playbackDone is closed when this run's runPlayback goroutine exits.
	playbackDone chan struct{}
	// PlayerStatus carries playback lifecycle updates to the UI. Buffered, and
	// drops rather than blocks when full.
	// Intended for a single long-lived consumer per player — the Discord voice
	// service's status watcher, or the CLI loop. Competing receivers steal
	// events from each other.
	PlayerStatus chan Status

	transportRecoveryMode TransportRecoveryMode
	transportSoftAttempts int

	// errMu protects lastPlaybackUserErr. It is separate from mu so the emit
	// paths, which already hold mu, cannot deadlock against a reader.
	errMu               sync.Mutex
	lastPlaybackUserErr string
	// onPlaybackFailed is set once at construction (Options.OnPlaybackFailed)
	// and never mutated, so the playback goroutine reads it without a lock.
	onPlaybackFailed func(guildID string, track parsers.Track, playbackErr error)
}

// Options configures optional Player behavior; the zero value is usable.
type Options struct {
	// Logger is optional. If zero, the player logs nothing.
	Logger zerolog.Logger
	// TransportRecoveryMode controls behavior on stream.ErrVoiceTransport.
	// Zero value (or any unknown value) means RecoveryHard.
	TransportRecoveryMode TransportRecoveryMode
	// TransportSoftAttempts bounds soft retries before falling back to hard
	// recovery. Applies to RecoverySoft only; default 1.
	TransportSoftAttempts int
	// OnPlaybackFailed fires when playback ends in an error after the track had
	// already started (a sink warm-up failure, say). guildID is empty for
	// non-Discord players, which should no-op rather than guess.
	OnPlaybackFailed func(guildID string, track parsers.Track, playbackErr error)
}

// New builds a Player with default options. There is no target here: one is
// supplied per playback via PlayNext(target), so a player outlives any single
// voice channel it plays into.
func New(sinkProvider sink.Provider, res Resolver) *Player {
	return NewWithOptions(sinkProvider, res, Options{})
}

// NewWithOptions builds a Player, normalizing the zero value of every option so
// Options{} is a valid argument: recovery falls back to RecoveryHard, soft
// attempts to 1, and an unset Logger to Nop. This is the only place the
// callback fields are written — see Options.
func NewWithOptions(sinkProvider sink.Provider, res Resolver, opts Options) *Player {
	mode := opts.TransportRecoveryMode
	if mode != RecoverySoft {
		mode = RecoveryHard
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
		queue:                 make([]parsers.Track, 0),
		stopPlayback:          make(chan struct{}),
		playbackDone:          make(chan struct{}),
		PlayerStatus:          make(chan Status, 10),
		transportRecoveryMode: mode,
		transportSoftAttempts: softAttempts,
		log:                   l,
		onPlaybackFailed:      opts.OnPlaybackFailed,
	}
}

// SetGuildID sets the Discord guild id, which the playback recorder is invoked
// with. Unset for the CLI.
func (p *Player) SetGuildID(guildID string) {
	p.mu.Lock()
	p.guildID = guildID
	p.mu.Unlock()
}

// SetRecorder sets an optional callback invoked once a track has actually
// produced audio (see PlaybackRecorder). Pass nil to disable.
func (p *Player) SetRecorder(r PlaybackRecorder) {
	p.mu.Lock()
	p.recorder = r
	p.mu.Unlock()
}

// Enqueue resolves input (a URL or a search query) and queues whatever it
// yields — one track, or a whole playlist. A resolve failure is both returned
// and emitted on PlayerStatus, because the caller that asked may not be the one
// rendering the result.
func (p *Player) Enqueue(input string, source string, parser string) error {
	p.log.Info().Str("input", input).Str("source", source).Str("parser", parser).Msg("enqueue_called")
	tracksInfo, err := p.resolver.Resolve(input, source, parser)
	if err != nil {
		p.log.Warn().Err(err).Msg("resolve_tracks_failed")
		p.emitPlaybackError(err)
		return err
	}

	return p.EnqueueTrackInfos(tracksInfo)
}

// EnqueueTrackInfo queues one already-resolved track, so a caller holding a
// TrackInfo does not pay for a second resolve.
func (p *Player) EnqueueTrackInfo(trackInfo sources.TrackInfo) error {
	return p.EnqueueTrackInfos([]sources.TrackInfo{trackInfo})
}

// EnqueueTrackInfos enqueues pre-resolved tracks as one batch. Batching is not
// just an optimization: PlayerStatus has a single consumer and drops when full,
// so a playlist enqueued one call at a time would emit a StatusAdded per track
// and flood it. Tracks without parsers are skipped; the call fails only when
// nothing at all could be queued.
func (p *Player) EnqueueTrackInfos(tracksInfo []sources.TrackInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tracks := make([]parsers.Track, 0, len(tracksInfo))
	for _, trackInfo := range tracksInfo {
		if len(trackInfo.AvailableParsers) == 0 {
			p.log.Warn().Str("title", trackInfo.Title).Msg("track_skipped_no_parsers")
			continue
		}
		tracks = append(tracks, parsers.Track{
			URL:           trackInfo.URL,
			Title:         trackInfo.Title,
			CurrentParser: trackInfo.AvailableParsers[0],
			SourceInfo:    trackInfo,
		})
	}
	if len(tracks) == 0 {
		p.emitPlaybackError(ErrNoParsersForTrack)
		return ErrNoParsersForTrack
	}

	p.queue = append(p.queue, tracks...)
	p.log.Info().Int("added", len(tracks)).Int("queue_len", len(p.queue)).Msg("queue_tracks_added")
	if p.currTrack != nil {
		p.emitStatus(StatusAdded)
	}
	return nil
}

// PlayNext stops current track (if any) and plays the next in queue.
// target is the voice channel ID for Discord, or "" for CLI.
func (p *Player) PlayNext(target string) error {
	p.log.Info().Int("queue_len", len(p.Queue())).Msg("play_next_called")
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
			p.mu.Lock()
			qEmpty := len(p.queue) == 0
			p.mu.Unlock()
			if qEmpty {
				// The Discord slash handler reports this one itself; emitting here
				// too would put a stray StatusError on the channel.
				return fmt.Errorf("%w: %v", ErrTrackStartFailed, err)
			}
			continue
		}

		// History is written from onParserConfirmed, once a parser has actually
		// produced audio — opening one proves nothing (see RecoveryStream.Open).
		p.log.Info().
			Str("title", track.Title).
			Str("parser", track.CurrentParser).
			Int("queue_len", len(p.Queue())).
			Msg("track_now_playing")
		return nil
	}
}

// Stop ends the current playback run. With disconnect it also clears the queue
// and releases the sink (leaving the voice channel); without it the queue
// survives, which is what a skip needs.
//
// It waits for the playback goroutine to actually exit before clearing state,
// capped at 10s so a wedged sink cannot block a stop forever, then mints fresh
// per-run channels. Idempotent: stopOnce means a second Stop during teardown is
// harmless.
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

// Pause is unimplemented by design: the sink owns the read loop, so there is no
// point above it at which packets can be held without the voice connection
// starving. Supporting it means giving the pause to the sink, not the player.
func (p *Player) Pause() error {
	return ErrPauseNotSupported
}

// Resume is unimplemented for the same reason as Pause: nothing was paused.
func (p *Player) Resume() error {
	return ErrResumeNotSupported
}

// IsPlaying reports whether a track is opening or actively playing.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing || p.starting
}

// CurrentTrack returns the track being opened or played, or nil when idle. The
// pointer identity is meaningful: a playback run owns its own *parsers.Track,
// compares against this to tell itself apart from a newer run (clearIfCurrent).
func (p *Player) CurrentTrack() *parsers.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currTrack
}

// Queue returns a snapshot of the waiting tracks. It is a clone, so a caller
// rendering it cannot be tripped up by a concurrent enqueue and cannot mutate
// the real queue by writing to the result.
func (p *Player) Queue() []parsers.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.queue)
}

func cloneTrack(tp parsers.Track) parsers.Track {
	out := tp
	if len(tp.SourceInfo.AvailableParsers) > 0 {
		out.SourceInfo.AvailableParsers = slices.Clone(tp.SourceInfo.AvailableParsers)
	}
	return out
}

// startTrack opens the track and hands it to a playback goroutine. It returns
// once the stream is open and StatusPlaying has been emitted — not when the
// track ends — so an Open failure is the caller's to report, while everything
// after it belongs to the goroutine.
//
// The per-run channels are minted here rather than in Stop, so a run always
// gets its own pair and a late Stop from the previous run cannot signal this
// one. Opening is deliberately outside the lock: it does network I/O.
func (p *Player) startTrack(track *parsers.Track, resumed bool) error {
	p.log.Info().
		Str("title", track.Title).
		Str("url", track.URL).
		Str("parser", track.CurrentParser).
		Int("queue_len", len(p.Queue())).
		Msg("playback_preparing")

	p.mu.Lock()
	p.stopPlayback = make(chan struct{})
	p.playbackDone = make(chan struct{})
	p.stopOnce = sync.Once{}
	p.starting = true
	p.playing = false
	p.currTrack = track
	p.announcedParser = ""
	p.recorded = false
	p.mu.Unlock()

	rs := stream.NewRecoveryStreamWithLogger(track, p.log)
	rs.SetOnParserConfirmed(func(parser string) { p.onParserConfirmed(track, parser) })
	if err := rs.Open(0); err != nil {
		p.log.Error().Err(err).Msg("stream_open_failed")
		p.mu.Lock()
		p.starting = false
		p.currTrack = nil
		p.mu.Unlock()
		return err
	}

	if resumed {
		p.clearPlaybackUserError()
		p.emitStatus(StatusResumed)
		p.log.Info().Str("title", track.Title).Str("parser", track.CurrentParser).Msg("track_resuming")
	} else {
		p.clearPlaybackUserError()
		p.emitStatus(StatusPlaying)
		p.log.Info().Str("title", track.Title).Str("parser", track.CurrentParser).Msg("track_starting")
	}

	p.mu.Lock()
	p.starting = false
	p.playing = true
	p.currTrack = track
	p.announcedParser = track.CurrentParser
	stopCh := p.stopPlayback
	doneCh := p.playbackDone
	p.mu.Unlock()

	// Completion chain: runPlayback -> this goroutine -> PlayNext -> startTrack
	// -> a fresh goroutine for the next track. Iteration happens via new
	// goroutines rather than recursion, so the stack does not grow with the
	// queue. On an empty queue PlayNext returns ErrNoTracksInQueue and the
	// Stop(true) below releases the sink.
	go func() {
		if err := p.runPlayback(track, rs, stopCh, doneCh); err != nil {
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

// maxVoiceTransportAttempts bounds sink rejoin plus Opus transport retries for
// one track (Discord gateway/voice). Distinct from RecoveryStream's media
// recovery, which counts parser attempts, not transport ones.
const maxVoiceTransportAttempts = 3

// runPlayback streams to the sink. track, stopCh and doneCh belong to this run
// alone: track must be the run's own pointer, because reading p.currTrack here
// could observe a newer run's track if this goroutine is scheduled late.
func (p *Player) runPlayback(track *parsers.Track, rs *stream.RecoveryStream, stopCh, doneCh chan struct{}) error {
	defer rs.Close()
	defer close(doneCh)

	p.mu.Lock()
	target := p.target
	recoveryMode := p.transportRecoveryMode
	softAttempts := p.transportSoftAttempts
	guildID := p.guildID
	p.mu.Unlock()

	// The buffered view is built once and reused across transport reopens, so the
	// read-ahead lead is not thrown away every time voice reconnects.
	packets := rs.Packets()

	failedSnapshot := cloneTrack(*track)
	p.log.Info().Str("title", track.Title).Str("parser", track.CurrentParser).Msg("playback_running")

	var err error
	softUsed := 0
	for attempt := 1; attempt <= maxVoiceTransportAttempts; attempt++ {
		var audioSink sink.AudioSink
		audioSink, err = p.sinkProvider.Sink(target)
		if err != nil {
			p.log.Warn().Int("attempt", attempt).Int("max", maxVoiceTransportAttempts).Err(err).Msg("sink_get_failed")
			p.sinkProvider.InvalidateSink()
			if attempt == maxVoiceTransportAttempts {
				p.markPlaybackFailed(track, failedSnapshot, guildID, errors.Join(ErrSinkUnavailable, fmt.Errorf("get sink: %w", err)))
				return errors.Join(ErrSinkUnavailable, fmt.Errorf("get sink: %w", err))
			}
			time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
			continue
		}

		err = audioSink.Stream(packets, stopCh)
		if err == nil {
			break
		}
		if errors.Is(err, stream.ErrPlaybackStopped) {
			p.clearIfCurrent(track)
			p.log.Info().Msg("playback_stopped_by_user")
			p.emitStatus(StatusStopped)
			return err
		}
		if errors.Is(err, stream.ErrVoiceTransport) {
			p.log.Warn().Int("attempt", attempt).Int("max", maxVoiceTransportAttempts).Err(err).Msg("voice_transport_error")

			softTry := recoveryMode == RecoverySoft && softUsed < softAttempts
			if softTry {
				softUsed++
				p.log.Info().Int("used", softUsed).Int("max", softAttempts).Msg("transport_recovery_soft_reopen_stream")
			} else {
				p.log.Info().Msg("transport_recovery_hard_invalidate_sink")
				p.sinkProvider.InvalidateSink()
			}

			if reopenErr := rs.ReopenAfterTransportFailure(); reopenErr != nil {
				p.markPlaybackFailed(track, failedSnapshot, guildID, fmt.Errorf("voice transport failed, could not reopen stream: %w", reopenErr))
				return fmt.Errorf("voice transport failed, could not reopen stream: %w", reopenErr)
			}
			if attempt == maxVoiceTransportAttempts {
				p.markPlaybackFailed(track, failedSnapshot, guildID, err)
				return err
			}
			time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
			continue
		}
		break
	}

	if err != nil {
		p.markPlaybackFailed(track, failedSnapshot, guildID, err)
		p.log.Warn().Err(err).Msg("playback_finished_error")
		return fmt.Errorf("playback error: %w", err)
	}

	p.clearIfCurrent(track)

	p.log.Info().Msg("playback_stopped")
	p.emitStatus(StatusStopped)

	// Queue-end disconnect is handled by the completion goroutine in startTrack:
	// PlayNext -> ErrNoTracksInQueue -> Stop(true). Keeping one decision point
	// avoids a double Stop(true)/ReleaseSink per track.
	return nil
}

// onParserConfirmed runs when RecoveryStream proves a parser is actually
// producing audio. It is the one point where "what is playing" becomes known,
// so it owns both the history row and the UI refresh: a mid-track switch (the
// previous parser opened, then died on its first read) re-emits StatusPlaying
// the Now Playing embed stops naming a parser that never played.
// Called from the playback goroutine; must not be called with p.mu held.
func (p *Player) onParserConfirmed(track *parsers.Track, parser string) {
	p.mu.Lock()
	if p.currTrack != track {
		p.mu.Unlock() // a newer run owns the player; this stream is being torn down
		return
	}
	// Compare against what the UI was told, not against the previous confirmation:
	// when the announced parser dies on its first read, the first confirmation
	// is already a different parser and still needs a redraw.
	stale := p.announcedParser != parser
	announced := p.announcedParser
	p.announcedParser = parser
	gid := p.guildID
	rec := p.recorder
	record := rec != nil && gid != "" && !p.recorded
	if record {
		p.recorded = true
	}
	p.mu.Unlock()

	if record {
		// Future: listened-duration aggregation would need completion callbacks,
		// from here or from runPlayback.
		rec.Record(gid, time.Now(), cloneTrack(*track))
	}
	if !stale {
		return // the UI already names this parser (same parser reopened)
	}
	// Not a duplicate of recovery's immediate_failure_switching_parser: that line
	// reports the media switch, this one reports its user-visible consequence --
	// the Now Playing embed still names `announced`, which never produced audio.
	p.log.Info().
		Str("title", track.Title).
		Str("announced", announced).
		Str("parser", parser).
		Msg("now_playing_parser_corrected")
	p.emitStatus(StatusPlaying)
}

func (p *Player) emitStatus(status Status) {
	select {
	case p.PlayerStatus <- status:
	default:
		// Nobody is draining the channel: the UI goes stale with no other signal.
		p.log.Warn().Str("status", string(status)).Msg("player_status_dropped")
	}
}

func (p *Player) clearPlaybackUserError() {
	p.errMu.Lock()
	p.lastPlaybackUserErr = ""
	p.errMu.Unlock()
}

func (p *Player) emitPlaybackError(err error) {
	if err != nil {
		p.errMu.Lock()
		p.lastPlaybackUserErr = err.Error()
		p.errMu.Unlock()
	}
	p.emitStatus(StatusError)
}

// clearIfCurrent resets playing state only if track is still the current one,
// so a stale run's goroutine cannot clobber the state of a newer run that has
// already started.
func (p *Player) clearIfCurrent(track *parsers.Track) {
	p.mu.Lock()
	if p.currTrack == track {
		p.playing = false
		p.currTrack = nil
	}
	p.mu.Unlock()
}

// markPlaybackFailed clears playing state, records the user-visible error and
// notifies Discord when that callback is wired.
func (p *Player) markPlaybackFailed(track *parsers.Track, failedSnapshot parsers.Track, guildID string, playbackErr error) {
	p.clearIfCurrent(track)
	if playbackErr == nil {
		return
	}
	p.emitPlaybackError(playbackErr)
	if p.onPlaybackFailed != nil && guildID != "" {
		p.onPlaybackFailed(guildID, failedSnapshot, playbackErr)
	}
}

// LastPlaybackUserError returns the last error string attached to StatusError,
// which may be empty.
func (p *Player) LastPlaybackUserError() string {
	p.errMu.Lock()
	s := p.lastPlaybackUserErr
	p.errMu.Unlock()
	return s
}

// ChannelID returns the current target: a voice channel id, or "" for the CLI.
func (p *Player) ChannelID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.target
}
