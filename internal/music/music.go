package music

import (
	"sync"
	"time"

	"github.com/keshon/melodix/internal/config"
	"github.com/keshon/melodix/internal/storage"
	"github.com/keshon/melodix/pkg/music/parsers"
	"github.com/keshon/melodix/pkg/music/player"
	"github.com/keshon/melodix/pkg/music/resolve"
	"github.com/keshon/melodix/pkg/music/sink"
	"github.com/keshon/melodix/pkg/music/sources"
	"github.com/rs/zerolog"
)

// Hooks are what a frontend plugs into the service. Only NewSink is required.
type Hooks struct {
	// NewSink builds the audio path for one scope. What it returns lives as
	// long as the scope's player does.
	NewSink func(scope string) sink.Provider

	// Watch is the player's single PlayerStatus consumer. It runs in its own
	// goroutine, started once when the scope's player is created -- which is
	// what keeps it the only one: competing receivers steal events from each
	// other. A frontend renders what a command did synchronously, in the
	// command, and leaves only the asynchronous transitions to Watch.
	Watch func(scope string, p *player.Player)

	// OnFailed reports playback that failed after the track had started.
	OnFailed func(scope string, track parsers.Track, err error)
}

// Service holds one player per scope -- a guild for the bot, the terminal for
// the CLI -- built the same way for both. It never starts playback: a frontend
// calls PlayNext itself, because how a start is announced is the frontend's
// business and Discord's announcement has to be ordered around it.
type Service struct {
	cfg   *config.Config
	store *storage.Storage
	log   zerolog.Logger
	hooks Hooks

	mu       sync.Mutex
	players  map[string]*player.Player
	sinks    map[string]sink.Provider
	resolver *resolve.Resolver
}

// New creates a service. store may be nil, in which case nothing is recorded
// to playback history.
func New(cfg *config.Config, store *storage.Storage, log zerolog.Logger, hooks Hooks) *Service {
	return &Service{
		cfg:      cfg,
		store:    store,
		log:      log,
		hooks:    hooks,
		players:  make(map[string]*player.Player),
		sinks:    make(map[string]sink.Provider),
		resolver: resolve.New(),
	}
}

// Player returns the scope's player, creating it on first use.
func (s *Service) Player(scope string) *player.Player {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.players[scope]; ok {
		return p
	}
	provider, ok := s.sinks[scope]
	if !ok {
		provider = s.hooks.NewSink(scope)
		s.sinks[scope] = provider
	}
	recoveryMode, ok := player.ParseTransportRecoveryMode(s.cfg.PlayerTransportRecoveryMode)
	if !ok {
		s.log.Warn().Str("value", s.cfg.PlayerTransportRecoveryMode).Msg("unknown_transport_recovery_mode_using_hard")
	}
	p := player.NewWithOptions(provider, s.resolver, player.Options{
		Logger:                s.log,
		TransportRecoveryMode: recoveryMode,
		TransportSoftAttempts: s.cfg.PlayerTransportSoftAttempts,
		OnPlaybackFailed:      s.hooks.OnFailed,
	})
	p.SetGuildID(scope)
	if s.store != nil {
		p.SetRecorder(recorder{store: s.store, log: s.log})
	}
	s.players[scope] = p
	if s.hooks.Watch != nil {
		go s.hooks.Watch(scope, p)
	}
	return p
}

// Resolve turns input into tracks with the resolver every player shares.
func (s *Service) Resolve(input, source, parser string) ([]sources.TrackInfo, error) {
	return s.resolver.Resolve(input, source, parser)
}

// StopAll stops every player and forgets it, leaving voice for the bot. Call
// on shutdown.
func (s *Service) StopAll() {
	s.mu.Lock()
	players := s.players
	s.players = make(map[string]*player.Player)
	s.sinks = make(map[string]sink.Provider)
	s.mu.Unlock()

	// In parallel, because each Stop leaves a voice channel and that waits on
	// the voice gateway. Sequentially, one guild whose gateway has stopped
	// answering spends the whole shutdown budget on its own and every guild
	// behind it is left in its channel -- and a server that has stopped
	// answering one connection is not answering the others either, so the
	// slow case is the case where they are all slow.
	var stopping sync.WaitGroup
	for _, p := range players {
		stopping.Add(1)
		go func(p *player.Player) {
			defer stopping.Done()
			_ = p.Stop(true)
		}(p)
	}
	stopping.Wait()
}

// InvalidateSinks drops every scope's current audio connection without
// stopping players or clearing queues. Intended for session restarts.
func (s *Service) InvalidateSinks() {
	s.mu.Lock()
	providers := make([]sink.Provider, 0, len(s.sinks))
	for _, p := range s.sinks {
		providers = append(providers, p)
	}
	s.mu.Unlock()

	for _, p := range providers {
		if p != nil {
			p.InvalidateSink()
		}
	}
}

// recorder appends every started track to the scope's playback history.
type recorder struct {
	store *storage.Storage
	log   zerolog.Logger
}

func (r recorder) Record(scope string, playedAt time.Time, track parsers.Track) {
	if _, err := r.store.AppendMusicPlayback(scope, track, playedAt); err != nil {
		r.log.Warn().Str("guild_id", scope).Err(err).Msg("playback_history_append_failed")
	}
}
