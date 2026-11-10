package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// Recovery owns fresh-session reservations across clients and process restarts.
// The storage directory must already be exclusively locked by the application.
// Reservations precede dialing, so failed handshakes count conservatively.
type Recovery struct {
	mu    sync.Mutex
	ctx   context.Context
	path  string
	limit int
	state recoveryState
	log   zerolog.Logger
	stage string
	fatal error
	now   func() time.Time
	fetch func(context.Context) (discord.SessionStartLimit, error)
	slot  chan struct{}
}

type recoveryState struct {
	Version     int         `json:"version"`
	Attempts    []time.Time `json:"attempts"`
	Next        time.Time   `json:"next"`
	RemoteTotal int         `json:"remote_total"`
	RemoteUsed  int         `json:"remote_used"`
	RemoteReset time.Time   `json:"remote_reset"`
	Checked     time.Time   `json:"checked"`
}

func NewRecovery(ctx context.Context, dir, token string, limit int, log zerolog.Logger) (*Recovery, error) {
	if limit < 20 || limit > 1000 {
		return nil, fmt.Errorf("DISCORD_SESSION_BUDGET must be between 20 and 1000")
	}
	r := &Recovery{ctx: ctx, path: filepath.Join(dir, "discord-recovery.json"), limit: limit, log: log, now: time.Now}
	r.slot = make(chan struct{}, 1)
	api := rest.New(rest.NewClient(token))
	r.fetch = func(ctx context.Context) (discord.SessionStartLimit, error) {
		v, err := api.GetGatewayBot(rest.WithCtx(ctx))
		if err != nil {
			return discord.SessionStartLimit{}, err
		}
		return v.SessionStartLimit, nil
	}
	b, err := os.ReadFile(r.path)
	if err == nil {
		if err = json.Unmarshal(b, &r.state); err != nil {
			return nil, fmt.Errorf("read recovery budget: %w", err)
		}
		if r.state.Version != 1 || r.state.RemoteTotal < 0 || r.state.RemoteUsed < 0 {
			return nil, errors.New("invalid recovery budget state")
		}
		for i, attempt := range r.state.Attempts {
			if attempt.IsZero() || (i > 0 && attempt.Before(r.state.Attempts[i-1])) {
				return nil, errors.New("invalid recovery reservation timestamps")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	r.state.Version = 1
	// Check Discord again after every process restart, retaining local usage.
	r.state.Checked = time.Time{}
	return r, nil
}

// Close is a no-op: client shutdown must not reset a process-wide budget.
func (r *Recovery) Close(context.Context) {}

func (r *Recovery) Fatal() error { r.mu.Lock(); defer r.mu.Unlock(); return r.fatal }

func (r *Recovery) save() error {
	b, err := json.Marshal(r.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.path), "recovery-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, r.path)
	}
	return err
}

// schedule uses the more exhausted of the local rolling allowance and Discord's
// shared allowance. RESUME never enters this limiter.
func (r *Recovery) schedule(now time.Time) (time.Time, string) {
	s := &r.state
	kept := s.Attempts[:0]
	for _, t := range s.Attempts {
		if t.Add(24 * time.Hour).After(now) {
			kept = append(kept, t)
		}
	}
	s.Attempts = kept
	next := s.Next
	stage := "normal"
	used := float64(len(kept)) / float64(r.limit)
	if s.RemoteTotal > 0 && now.Before(s.RemoteReset) {
		used = math.Max(used, float64(s.RemoteUsed)/float64(s.RemoteTotal))
	}
	if len(kept) >= int(math.Ceil(.95*float64(r.limit))) {
		i := len(kept) - int(math.Ceil(.95*float64(r.limit)))
		next = later(next, kept[i].Add(24*time.Hour))
		stage = "paused"
	}
	if s.RemoteTotal > 0 && float64(s.RemoteUsed) >= .95*float64(s.RemoteTotal) && now.Before(s.RemoteReset) {
		next = later(next, s.RemoteReset)
		stage = "paused"
	}
	if stage != "paused" && used >= .75 {
		stage = "throttled"
		delay := 5*time.Second + time.Duration(math.Min(1, (used-.75)/.15)*float64(5*time.Minute-5*time.Second))
		if used >= .90 {
			stage = "degraded"
			delay = 5*time.Minute + time.Duration(math.Min(1, (used-.90)/.05)*float64(10*time.Minute))
		}
		if len(kept) > 0 {
			next = later(next, kept[len(kept)-1].Add(delay))
		}
	}
	if len(kept) >= 5 && kept[len(kept)-5].Add(10*time.Minute).After(kept[len(kept)-1]) && kept[len(kept)-1].Add(15*time.Minute).After(now) {
		next = later(next, kept[len(kept)-1].Add(15*time.Minute))
		if stage != "paused" {
			stage = "cooldown"
		}
	}
	return next, stage
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (r *Recovery) Wait(ctx context.Context, _ int) error {
	// Hold one admission slot until the handshake ends and Unlock is called.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ctx.Done():
		return r.ctx.Err()
	case r.slot <- struct{}{}:
	}
	if err := r.wait(ctx, true); err != nil {
		<-r.slot
		return err
	}
	return nil
}

// WaitUntilEligible avoids rebuilding clients during a long budget cooldown.
// Admission is still checked and reserved by the Gateway's limiter afterward.
func (r *Recovery) WaitUntilEligible(ctx context.Context) error {
	return r.wait(ctx, false)
}

func (r *Recovery) wait(ctx context.Context, reserve bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		if r.fatal != nil {
			err := r.fatal
			r.mu.Unlock()
			return err
		}
		now := r.now()
		next, stage := r.schedule(now)
		if next.After(now) {
			r.report(ctx, stage, next)
			r.mu.Unlock()
			timer := time.NewTimer(next.Sub(now))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				continue
			}
		}
		// Refresh before admission. Never connect on an unknown budget.
		if r.state.Checked.IsZero() || now.Sub(r.state.Checked) >= time.Minute || !now.Before(r.state.RemoteReset) {
			fetchCtx, done := context.WithTimeout(ctx, 10*time.Second)
			v, err := r.fetch(fetchCtx)
			done()
			if err != nil {
				if IsTerminal(err) {
					r.fatal = err
				}
				r.state.Next = later(r.state.Next, now.Add(time.Minute))
				r.report(ctx, "budget-unavailable", r.state.Next)
				r.mu.Unlock()
				return fmt.Errorf("refresh session budget: %w", err)
			}
			if v.Total <= 0 || v.Remaining < 0 || v.Remaining > v.Total || v.ResetAfter < 0 || (v.ResetAfter == 0 && v.Remaining != v.Total) || v.MaxConcurrency <= 0 {
				r.fatal = fmt.Errorf("discord returned an invalid session budget: total=%d remaining=%d reset_after=%d max_concurrency=%d", v.Total, v.Remaining, v.ResetAfter, v.MaxConcurrency)
				r.mu.Unlock()
				return r.Fatal()
			}
			used := v.Total - v.Remaining
			if now.Before(r.state.RemoteReset) {
				used = max(used, r.state.RemoteUsed)
			}
			r.state.RemoteTotal = v.Total
			r.state.RemoteUsed = used
			// Discord reports a zero reset timer for an unused allowance.
			// Keep a conservative window until the next refresh supplies a timer.
			resetAfter := time.Duration(v.ResetAfter) * time.Millisecond
			if resetAfter == 0 {
				resetAfter = 24 * time.Hour
			}
			r.state.RemoteReset = now.Add(resetAfter)
			r.state.Checked = now
			if err = r.save(); err != nil {
				r.fatal = err
				r.mu.Unlock()
				return err
			}
			r.mu.Unlock()
			continue
		}
		if !reserve {
			r.mu.Unlock()
			return nil
		}
		r.state.Attempts = append(r.state.Attempts, now)
		r.state.RemoteUsed++
		r.state.Next = now.Add(5 * time.Second)
		if err := r.save(); err != nil {
			r.fatal = err
			r.mu.Unlock()
			return err
		}
		r.log.Info().Int("reserved_24h", len(r.state.Attempts)).Int("local_budget", r.limit).Int("discord_remaining", max(0, r.state.RemoteTotal-r.state.RemoteUsed)).Msg("discord_session_reserved")
		r.mu.Unlock()
		return nil
	}
}

func (r *Recovery) Unlock(_ int) {
	defer func() { <-r.slot }()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Next = later(r.state.Next, r.now().Add(5*time.Second))
	if err := r.save(); err != nil {
		r.fatal = err
	}
}

// report records transitions even when the Gateway is offline.
func (r *Recovery) report(_ context.Context, stage string, next time.Time) {
	if stage == r.stage {
		return
	}
	r.stage = stage
	r.log.Warn().Str("state", stage).Time("next_attempt", next).Msg("discord_recovery_status")
}

// Presence describes protection without suggesting that playback is broken.
func (r *Recovery) Presence() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, stage := r.schedule(r.now())
	switch stage {
	case "paused":
		return "Connection budget low | retries paused"
	case "cooldown":
		return "Connection recovery | cooling down"
	case "throttled", "degraded":
		return "Connection budget low | retries slowed"
	default:
		return ""
	}
}

func (r *Recovery) Online() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log.Info().Msg("discord_gateway_online")
	r.stage = "online"
}

func IsTerminal(err error) bool {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return !gateway.CloseEventCodeByCode(closeErr.Code).Reconnect
	}
	var apiErr *rest.Error
	return errors.As(err, &apiErr) && apiErr.Response != nil && (apiErr.Response.StatusCode == 401 || apiErr.Response.StatusCode == 403)
}
