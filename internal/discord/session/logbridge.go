package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// bridge carries disgo's and dave-go's slog records into the app's zerolog
// logger, as records rather than as text.
//
// The first version of this was an io.Writer under slog's TextHandler, which
// meant every decision had to be made by searching a formatted line for a
// phrase -- and one of those decisions was severity, which it got wrong: the
// level was in the text it was matching against, so a gateway ERROR was
// reported as INF and an outage read like idle chatter. A Handler is given the
// record, so the level is the level and a field is a field.
type bridge struct {
	log   zerolog.Logger
	attrs []slog.Attr
	// frames aggregates the one record dave-go emits per encrypted frame; nil
	// lets those through individually.
	frames *frameCounter
}

func (b *bridge) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slogLevel(b.log.GetLevel())
}

func (b *bridge) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *b
	next.attrs = append(append([]slog.Attr{}, b.attrs...), attrs...)
	return &next
}

// WithGroup is unused: neither library groups its attributes, and a bridge
// that pretended to support it would be untested code answering a question
// nobody asks.
func (b *bridge) WithGroup(string) slog.Handler { return b }

func (b *bridge) Handle(_ context.Context, r slog.Record) error {
	if b.frames != nil && r.Message == frameEncrypted {
		b.frames.count(b.log, r)
		return nil
	}

	event := b.event(r).Str("msg", r.Message)
	for _, a := range b.attrs {
		event = event.Interface(a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		event = event.Interface(a.Key, a.Value.Any())
		return true
	})
	if r.Message == audioSendFailure {
		// Not a fix, and should not be mistaken for one. disgo logs a UDP
		// write failure that is not a closed socket and carries on pulling at
		// 50Hz, so a whole track can be drained into a socket delivering
		// nothing while everything above reports normal playback. Melodix
		// cannot observe that error any other way and cannot act on it at all
		// -- so it is at least given a name worth counting and alerting on,
		// rather than being one line of library prose among thousands.
		event.Msg("voice_audio_send_failed")
		return nil
	}
	// Everything else is prose melodix cannot act on, filed under one name so
	// it stays greppable.
	event.Msg("library_log")
	return nil
}

// event picks the zerolog level, which is the whole reason this is a Handler.
func (b *bridge) event(r slog.Record) *zerolog.Event {
	switch {
	case r.Level >= slog.LevelError:
		return b.log.Error()
	case r.Level >= slog.LevelWarn:
		return b.log.Warn()
	case r.Level >= slog.LevelInfo:
		return b.log.Info()
	default:
		return b.log.Debug()
	}
}

// audioSendFailure is disgo's own wording for a UDP write it could not make
// and did not act on (voice/audio_sender.go, handleErr). Matched because it
// reaches us as a message and nothing else: the sender logs it and returns, so
// there is no error value anywhere for melodix to catch.
const audioSendFailure = "failed to send audio"

// frameEncrypted is dave-go's per-frame debug record. It is one per 20ms of
// audio -- fifty a second, six and a half thousand in a two-minute track --
// and at LOG_LEVEL=debug it buries everything else in the file and everything
// else on the console.
//
// It is not noise, though. Whether a frame went out under the live epoch or
// under the previous one is part of the answer to "can the people in this
// channel actually hear this", and that question has already been wrong twice
// here. So it is counted, not dropped.
const frameEncrypted = "frame encrypted"

// frameSummaryEvery bounds how often the tally is reported: long enough that a
// track produces a handful of lines rather than thousands, short enough that a
// re-key window still shows up as one of them.
const frameSummaryEvery = 30 * time.Second

// frameCounter turns dave-go's per-frame record into a periodic tally.
//
// It counts only what that record says. A frame sent with no encryption at all
// is not in here, because dave-go's passthrough path logs nothing -- it
// increments Stats().PassthroughFrames and returns. That case is the send
// path's to report, and voicesink's own tally does.
type frameCounter struct {
	mu       sync.Mutex
	total    int
	retained int
	since    time.Time
}

func (c *frameCounter) count(log zerolog.Logger, r slog.Record) {
	retained := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "retained" {
			retained = a.Value.Bool()
			return false
		}
		return true
	})

	c.mu.Lock()
	if c.since.IsZero() {
		c.since = time.Now()
	}
	c.total++
	if retained {
		c.retained++
	}
	window := time.Since(c.since)
	if window < frameSummaryEvery {
		c.mu.Unlock()
		return
	}
	total, underPrevious := c.total, c.retained
	c.total, c.retained, c.since = 0, 0, time.Time{}
	c.mu.Unlock()

	// under_previous_epoch is the one worth watching. dave-go keeps the
	// previous epoch's send key for ten seconds after a re-key, deliberately,
	// so listeners who have not processed the transition yet keep hearing
	// audio -- but it skips that when the new epoch added a member, because
	// somebody who was never in the old epoch holds none of its keys and would
	// hear nothing for the whole window. A count that stays high across a join
	// is that skip having failed.
	log.Info().
		Int("frames", total).
		Int("under_previous_epoch", underPrevious).
		Dur("window", window).
		Msg("voice_frames_encrypted")
}

// slogLevel maps the app's configured level onto slog's, so LOG_LEVEL reaches
// the libraries rather than each library deciding for itself.
func slogLevel(l zerolog.Level) slog.Level {
	switch l {
	case zerolog.TraceLevel, zerolog.DebugLevel:
		return slog.LevelDebug
	case zerolog.InfoLevel:
		return slog.LevelInfo
	case zerolog.WarnLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// SlogLogger is the app logger in the shape disgo's own subsystems take, for
// callers that configure one (the voice manager) outside New.
func SlogLogger(log zerolog.Logger) *slog.Logger {
	return slog.New(&bridge{log: log})
}
